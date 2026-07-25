package consumercontract_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/jmo/terminal-redeemer/internal/config"
)

type contractFile struct {
	SchemaVersion   int    `json:"schema_version"`
	ContractID      string `json:"contract_id"`
	ContractVersion string `json:"contract_version"`
	Protocol        struct {
		InventorySchemaVersions  []int  `json:"inventory_schema_versions"`
		RPCSchemaVersions        []int  `json:"rpc_schema_versions"`
		ControllerSchemaVersions []int  `json:"controller_schema_versions"`
		WorkspaceNormalization   string `json:"workspace_normalization"`
	} `json:"protocol"`
	Compatibility struct {
		NiriVersion   string `json:"niri_version"`
		ZellijVersion string `json:"zellij_version"`
		Topology      string `json:"topology"`
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
	Authority struct {
		SupportedModes                []string `json:"supported_modes"`
		ConvergedProperties           []string `json:"converged_properties"`
		LocalSupportedDriftPolicy     string   `json:"local_supported_drift_policy"`
		OrderPolicy                   string   `json:"order_policy"`
		LeechLocationConfigurable     bool     `json:"leech_location_configurable"`
		HostSpatialWritebackAvailable bool     `json:"host_spatial_writeback_available"`
	} `json:"authority"`
	Drops struct {
		Key                                   string   `json:"key"`
		SurvivesSourceReplacement             bool     `json:"survives_source_replacement"`
		SurvivesSourceEpochReplacement        bool     `json:"survives_source_epoch_replacement"`
		SurvivesHeadlessWhileSessionLive      bool     `json:"survives_headless_while_session_live"`
		EarlyClearOperations                  []string `json:"early_clear_operations"`
		AutomaticExpiryRequires               []string `json:"automatic_expiry_requires"`
		NonAuthoritativeObservationsNoAdvance bool     `json:"non_authoritative_observations_do_not_advance"`
	} `json:"drops"`
	Revisions struct {
		CompletePollAdvancesRevision    bool     `json:"complete_poll_advances_revision"`
		UnchangedSemanticsStillAdvances bool     `json:"unchanged_semantics_still_advances_revision"`
		SameRevisionSameSemantics       string   `json:"same_revision_same_semantics"`
		SameRevisionDifferentSemantics  string   `json:"same_revision_different_semantics"`
		NonAuthoritativeObservations    []string `json:"non_authoritative_observations"`
	} `json:"revisions"`
	Commands      map[string][]string `json:"commands"`
	Module        map[string]string   `json:"module"`
	Configuration struct {
		Namespace          string   `json:"namespace"`
		TypedOptions       []string `json:"typed_options"`
		ReadOnlyOptions    []string `json:"read_only_options"`
		UnsupportedOptions []string `json:"unsupported_options"`
	} `json:"configuration"`
	Integration struct {
		LaunchHelperOption             string `json:"launch_helper_option"`
		CloseHelperOption              string `json:"close_helper_option"`
		GeneratedNiriOption            string `json:"generated_niri_option"`
		PackagedNiriPath               string `json:"packaged_niri_path"`
		LaunchBinding                  string `json:"launch_binding"`
		CloseBinding                   string `json:"close_binding"`
		BindingsInstalledAutomatically bool   `json:"bindings_installed_automatically"`
	} `json:"integration"`
	Limitations struct {
		ZellijSharedMinimumClientGridReflow        bool   `json:"zellij_shared_minimum_client_grid_reflow"`
		SpatialPlacement                           string `json:"spatial_placement"`
		PinnedVersionCoupling                      bool   `json:"pinned_version_coupling"`
		AmbiguousTransportMayHaveCreatedHostWork   bool   `json:"ambiguous_transport_may_have_created_host_work"`
		RoutedLaunchProcessCorrelationCanBePending bool   `json:"routed_launch_process_correlation_can_remain_pending"`
	} `json:"limitations"`
	Rollout    map[string]bool `json:"rollout"`
	FutureWork []string        `json:"future_work"`
}

func contractDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "contracts", "host-leech-slices", "v1")
}

func decodeStrict[T any](t *testing.T, path string, target *T) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("trailing JSON in %s", path)
	}
	return raw
}

func TestConsumerContractDefaultsAndCompatibilityMatchRuntime(t *testing.T) {
	base := contractDir(t)
	var contract contractFile
	decodeStrict(t, filepath.Join(base, "consumer-contract.json"), &contract)

	cfg := config.Defaults()
	got := contract.Defaults
	if got.LeechModeEnabled != cfg.Slice.LeechModeEnabled || got.ControllerEnabled != cfg.Slice.Controller.Enabled || got.SliceClipboardEnabled != cfg.Slice.Clipboard.Enabled || got.AuthorityMode != cfg.Slice.Controller.AuthorityMode || got.LeechWriteAuthorized != cfg.Slice.Controller.LeechWriteAuthorized || got.PollInterval != cfg.Slice.Controller.PollInterval.String() || got.ControlTimeout != cfg.Slice.Controller.ControlTimeout.String() || got.RetryWindow != cfg.Slice.Controller.RetryWindow.String() || got.SourceGoneGrace != cfg.Slice.Controller.SourceGoneGrace.String() || got.SourceGoneConfirmations != cfg.Slice.Controller.SourceGoneConfirmations {
		t.Fatalf("contract defaults drifted from runtime: contract=%#v runtime=%#v", got, cfg.Slice)
	}
	if !reflect.DeepEqual(contract.Protocol.InventorySchemaVersions, []int{1}) || !reflect.DeepEqual(contract.Protocol.RPCSchemaVersions, []int{1}) || !reflect.DeepEqual(contract.Protocol.ControllerSchemaVersions, []int{2}) {
		t.Fatalf("protocol schema surface drifted: %#v", contract.Protocol)
	}
	if contract.Compatibility.NiriVersion != cfg.Slice.ExpectedNiriVersion || contract.Compatibility.ZellijVersion != "0.43.1" {
		t.Fatalf("pinned compatibility drifted: %#v", contract.Compatibility)
	}
	if !reflect.DeepEqual(contract.Authority.SupportedModes, []string{"host_location"}) || contract.Authority.LocalSupportedDriftPolicy != "revert_to_host" || contract.Authority.LeechLocationConfigurable || contract.Authority.HostSpatialWritebackAvailable {
		t.Fatalf("v1 authority surface drifted: %#v", contract.Authority)
	}
	if contract.Drops.Key != "exact_verified_zellij_session_id" || !contract.Drops.SurvivesSourceReplacement || !contract.Drops.SurvivesSourceEpochReplacement || !contract.Drops.SurvivesHeadlessWhileSessionLive || !contract.Drops.NonAuthoritativeObservationsNoAdvance {
		t.Fatalf("session drop surface drifted: %#v", contract.Drops)
	}
	if !contract.Revisions.CompletePollAdvancesRevision || !contract.Revisions.UnchangedSemanticsStillAdvances || contract.Revisions.SameRevisionSameSemantics != "idempotent_replay" || contract.Revisions.SameRevisionDifferentSemantics != "conflict" {
		t.Fatalf("revision surface drifted: %#v", contract.Revisions)
	}
	if !contract.Limitations.ZellijSharedMinimumClientGridReflow || contract.Limitations.SpatialPlacement != "approximate_proportional" || !contract.Limitations.PinnedVersionCoupling || !contract.Limitations.AmbiguousTransportMayHaveCreatedHostWork || !contract.Limitations.RoutedLaunchProcessCorrelationCanBePending {
		t.Fatalf("limitation surface drifted: %#v", contract.Limitations)
	}
	wantCommands := map[string][]string{
		"inventory_init":        {"$REDEEM", "slice", "inventory", "init"},
		"inventory_snapshot":    {"$REDEEM", "slice", "inventory", "snapshot", "--accept-schema-version", "1"},
		"rpc":                   {"$REDEEM", "slice", "rpc"},
		"launch":                {"$REDEEM", "slice", "launch"},
		"launch_reconnect":      {"$REDEEM", "slice", "launch", "--reconnect-token", "$TOKEN"},
		"close_focused":         {"$REDEEM", "slice", "close-focused"},
		"controller_init":       {"$REDEEM", "slice", "controller", "init"},
		"controller_run":        {"$REDEEM", "slice", "controller", "run"},
		"controller_status":     {"$REDEEM", "slice", "controller", "status"},
		"controller_operations": {"workspace-add", "workspace-remove", "pickup", "drop", "close", "reopen", "undo", "reconnect", "launch-handoff"},
		"mode_enable":           {"$REDEEM", "slice", "mode", "enable"},
		"mode_disable":          {"$REDEEM", "slice", "mode", "disable"},
		"mode_status":           {"$REDEEM", "slice", "mode", "status"},
		"legacy_attach":         {"$REDEEM", "mirror", "open", "--mode", "attach"},
	}
	if !reflect.DeepEqual(contract.Commands, wantCommands) {
		t.Fatalf("complete helper argv surface drifted: got=%q want=%q", contract.Commands, wantCommands)
	}
	wantModule := map[string]string{
		"home_manager":     "homeManagerModules.terminal-redeemer",
		"nixos":            "nixosModules.terminal-redeemer",
		"package":          "packages.x86_64-linux.terminal-redeemer",
		"app":              "apps.x86_64-linux.redeem",
		"contract_package": "packages.x86_64-linux.host-leech-consumer-contract",
		"contract_library": "lib.sliceConsumerContract",
	}
	if !reflect.DeepEqual(contract.Module, wantModule) {
		t.Fatalf("complete module surface drifted: got=%q want=%q", contract.Module, wantModule)
	}
	wantRollout := map[string]bool{
		"installs_niri_bindings":                       false,
		"legacy_attach_retained":                       true,
		"watch_supported":                              false,
		"automatic_local_fallback_after_remote_intent": false,
	}
	if !reflect.DeepEqual(contract.Rollout, wantRollout) {
		t.Fatalf("complete rollout surface drifted: got=%v want=%v", contract.Rollout, wantRollout)
	}
	wantFuture := []string{"exact_live_column_order", "multi_monitor_topology", "slice_clipboard_sync", "named_slices", "read_only_watch_projection"}
	if !reflect.DeepEqual(contract.FutureWork, wantFuture) {
		t.Fatalf("future-work surface drifted: got=%q want=%q", contract.FutureWork, wantFuture)
	}
}
