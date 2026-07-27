package consumercontract_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/jmo/terminal-redeemer/internal/config"
	"github.com/jmo/terminal-redeemer/internal/slicecontroller"
	"github.com/jmo/terminal-redeemer/internal/sliceprotocol"
	"github.com/jmo/terminal-redeemer/internal/slicerpc"
	"github.com/jmo/terminal-redeemer/internal/zellijlive"
)

type runtimeContract struct {
	Protocol struct {
		InventorySchemaVersions  []int  `json:"inventory_schema_versions"`
		RPCSchemaVersions        []int  `json:"rpc_schema_versions"`
		ControllerSchemaVersions []int  `json:"controller_schema_versions"`
		WorkspaceNormalization   string `json:"workspace_normalization"`
	} `json:"protocol"`
	Compatibility struct {
		NiriVersion   string `json:"niri_version"`
		ZellijVersion string `json:"zellij_version"`
	} `json:"compatibility"`
	Defaults struct {
		LeechModeEnabled        bool   `json:"leech_mode_enabled"`
		ControllerEnabled       bool   `json:"controller_enabled"`
		SliceClipboardEnabled   bool   `json:"slice_clipboard_enabled"`
		AuthorityMode           string `json:"authority_mode"`
		LeechWriteAuthorized    bool   `json:"leech_write_authorized"`
		PollInterval            string `json:"poll_interval"`
		ControlTimeout          string `json:"control_timeout"`
		RetryWindow             string `json:"retry_window"`
		SourceGoneGrace         string `json:"source_gone_grace"`
		SourceGoneConfirmations int    `json:"source_gone_confirmations"`
	} `json:"defaults"`
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(workingDir, "..", ".."))
}

func contractDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repositoryRoot(t), "contracts", "host-leech-slices", "v1")
}

func readStrictJSON(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(contractDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	if err := sliceprotocol.RejectDuplicateKeys(raw); err != nil {
		t.Fatalf("strict JSON validation of %s: %v", name, err)
	}
	return raw
}

func TestConsumerContractRuntimeValues(t *testing.T) {
	raw := readStrictJSON(t, "consumer-contract.json")
	var contract runtimeContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}

	if got, want := contract.Protocol.InventorySchemaVersions, []int{int(sliceprotocol.SchemaVersion)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("inventory schema versions=%v, want runtime %v", got, want)
	}
	if got, want := contract.Protocol.RPCSchemaVersions, []int{int(slicerpc.SchemaVersion)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RPC schema versions=%v, want runtime %v", got, want)
	}
	if got, want := contract.Protocol.ControllerSchemaVersions, []int{slicecontroller.SchemaVersion}; !reflect.DeepEqual(got, want) {
		t.Fatalf("controller schema versions=%v, want runtime %v", got, want)
	}
	if contract.Protocol.WorkspaceNormalization != sliceprotocol.WorkspaceNormalization {
		t.Fatalf("workspace normalization=%q, want runtime %q", contract.Protocol.WorkspaceNormalization, sliceprotocol.WorkspaceNormalization)
	}

	cfg := config.Defaults()
	got := contract.Defaults
	if got.LeechModeEnabled != cfg.Slice.LeechModeEnabled ||
		got.ControllerEnabled != cfg.Slice.Controller.Enabled ||
		got.SliceClipboardEnabled != cfg.Slice.Clipboard.Enabled ||
		got.AuthorityMode != cfg.Slice.Controller.AuthorityMode ||
		got.LeechWriteAuthorized != cfg.Slice.Controller.LeechWriteAuthorized ||
		got.PollInterval != cfg.Slice.Controller.PollInterval.String() ||
		got.ControlTimeout != cfg.Slice.Controller.ControlTimeout.String() ||
		got.RetryWindow != cfg.Slice.Controller.RetryWindow.String() ||
		got.SourceGoneGrace != cfg.Slice.Controller.SourceGoneGrace.String() ||
		got.SourceGoneConfirmations != cfg.Slice.Controller.SourceGoneConfirmations {
		t.Fatalf("contract defaults drifted from runtime: contract=%#v runtime=%#v", got, cfg.Slice)
	}
	if contract.Compatibility.NiriVersion != cfg.Slice.ExpectedNiriVersion || contract.Compatibility.ZellijVersion != zellijlive.PinnedVersion {
		t.Fatalf("pinned compatibility drifted: contract=%#v runtime niri=%q zellij=%q", contract.Compatibility, cfg.Slice.ExpectedNiriVersion, zellijlive.PinnedVersion)
	}
}

func TestConsumerContractStrictJSON(t *testing.T) {
	tests := []struct {
		name         string
		duplicateKey string
	}{
		{name: "consumer-contract.json", duplicateKey: `"schema_version":0,`},
		{name: "consumer-contract.schema.json", duplicateKey: `"$schema":"duplicate",`},
	}
	for _, tc := range tests {
		raw := readStrictJSON(t, tc.name)
		if len(raw) == 0 || raw[0] != '{' {
			t.Fatalf("%s is not a JSON object", tc.name)
		}
		mutated := append([]byte("{"+tc.duplicateKey), raw[1:]...)
		if err := sliceprotocol.RejectDuplicateKeys(mutated); err == nil {
			t.Fatalf("production strict JSON gate accepted duplicate key in %s", tc.name)
		}
	}
}

func validateRepositoryRelativeMarkdownLinks(root string) error {
	documents := []string{filepath.Join(root, "README.md")}
	if err := filepath.WalkDir(filepath.Join(root, "docs"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			documents = append(documents, path)
		}
		return nil
	}); err != nil {
		return err
	}

	markdownLink := regexp.MustCompile(`\[[^]]*\]\(([^)]+)\)`)
	for _, document := range documents {
		raw, err := os.ReadFile(document)
		if err != nil {
			return err
		}
		for _, match := range markdownLink.FindAllStringSubmatch(string(raw), -1) {
			target := strings.TrimSpace(match[1])
			if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
				target = strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">")
			}
			target = strings.SplitN(target, " ", 2)[0]
			target = strings.SplitN(target, "#", 2)[0]
			if target == "" || filepath.IsAbs(target) || strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(document), filepath.FromSlash(target))); err != nil {
				relativeDocument, relErr := filepath.Rel(root, document)
				if relErr != nil {
					relativeDocument = document
				}
				return fmt.Errorf("%s: relative Markdown link %q: %w", relativeDocument, target, err)
			}
		}
	}
	return nil
}

func TestRepositoryRelativeMarkdownLinks(t *testing.T) {
	if err := validateRepositoryRelativeMarkdownLinks(repositoryRoot(t)); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryRelativeMarkdownLinksRejectMissingTarget(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("[missing](docs/not-present.md)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateRepositoryRelativeMarkdownLinks(root); err == nil {
		t.Fatal("missing repository-relative Markdown link was accepted")
	}
}

func TestConsumerContractSourcePackageMembers(t *testing.T) {
	base := contractDir(t)
	for _, name := range []string{
		"consumer-contract.json",
		"consumer-contract.schema.json",
		"niri-bindings.kdl.in",
	} {
		info, err := os.Stat(filepath.Join(base, name))
		if err != nil {
			t.Errorf("required contract member %s: %v", name, err)
		} else if !info.Mode().IsRegular() {
			t.Errorf("required contract member %s is not a regular file", name)
		}
	}
}
