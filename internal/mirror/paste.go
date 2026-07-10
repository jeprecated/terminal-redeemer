package mirror

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PasteConfig struct {
	SourceHost       string
	SSHCommand       string
	SSHOptions       []string
	SCPCommand       string
	SCPOptions       []string
	ClipboardCommand string
	KittyCommand     string
	KittyTo          string
	TempDir          string
	MIMETypes        []string
}

type PasteResult struct {
	Image      bool
	MIMEType   string
	RemotePath string
	FellBack   bool
}

type PasteBridge struct {
	Runner Runner
	ID     func() (string, error)
}

func (bridge PasteBridge) Paste(ctx context.Context, cfg PasteConfig) (PasteResult, error) {
	if err := ValidateDestination(cfg.SourceHost); err != nil {
		return PasteResult{}, err
	}
	if strings.TrimSpace(cfg.KittyTo) == "" {
		return PasteResult{}, fmt.Errorf("paste-image requires --kitty-to or KITTY_LISTEN_ON")
	}
	if !filepath.IsAbs(cfg.TempDir) {
		return PasteResult{}, fmt.Errorf("clipboard temp directory must be absolute")
	}
	runner := bridge.Runner
	if runner == nil {
		runner = ExecRunner{}
	}

	types, err := runner.Output(ctx, Command{Name: cfg.ClipboardCommand, Args: []string{"--list-types"}})
	if err != nil {
		if fallbackErr := sendKittyText(ctx, runner, cfg.KittyCommand, cfg.KittyTo, []byte{0x16}); fallbackErr != nil {
			return PasteResult{}, fmt.Errorf("clipboard unavailable (%v) and Ctrl-V fallback failed: %w", err, fallbackErr)
		}
		return PasteResult{FellBack: true}, nil
	}
	mime, ext := chooseImageMIME(types, cfg.MIMETypes)
	if mime == "" {
		if err := sendKittyText(ctx, runner, cfg.KittyCommand, cfg.KittyTo, []byte{0x16}); err != nil {
			return PasteResult{}, fmt.Errorf("forward Ctrl-V: %w", err)
		}
		return PasteResult{FellBack: true}, nil
	}
	image, err := runner.Output(ctx, Command{Name: cfg.ClipboardCommand, Args: []string{"--type", mime}})
	if err != nil || len(image) == 0 {
		if fallbackErr := sendKittyText(ctx, runner, cfg.KittyCommand, cfg.KittyTo, []byte{0x16}); fallbackErr != nil {
			return PasteResult{}, fmt.Errorf("read clipboard image (%v) and Ctrl-V fallback failed: %w", err, fallbackErr)
		}
		return PasteResult{FellBack: true}, nil
	}

	id := bridge.ID
	if id == nil {
		id = RandomID
	}
	unique, err := id()
	if err != nil {
		return PasteResult{}, fmt.Errorf("create clipboard image name: %w", err)
	}
	path := filepath.Join(filepath.Clean(cfg.TempDir), "redeem-clipboard-"+unique+"."+ext)
	if err := os.WriteFile(path, image, 0o600); err != nil {
		return PasteResult{}, fmt.Errorf("write clipboard image: %w", err)
	}
	defer func() { _ = os.Remove(path) }()

	mkdirArgs := append([]string(nil), cfg.SSHOptions...)
	mkdirArgs = append(mkdirArgs, "--", cfg.SourceHost, "mkdir -p -- "+ShellQuote(filepath.Dir(path)))
	if err := runner.Run(ctx, Command{Name: cfg.SSHCommand, Args: mkdirArgs}); err != nil {
		return PasteResult{}, fmt.Errorf("create remote clipboard directory: %w", err)
	}
	scpArgs := append([]string(nil), cfg.SCPOptions...)
	scpArgs = append(scpArgs, "--", path, cfg.SourceHost+":"+path)
	if err := runner.Run(ctx, Command{Name: cfg.SCPCommand, Args: scpArgs}); err != nil {
		return PasteResult{}, fmt.Errorf("copy clipboard image to source: %w", err)
	}
	if err := sendKittyText(ctx, runner, cfg.KittyCommand, cfg.KittyTo, []byte(path)); err != nil {
		return PasteResult{}, fmt.Errorf("inject remote clipboard path: %w", err)
	}
	return PasteResult{Image: true, MIMEType: mime, RemotePath: path}, nil
}

func chooseImageMIME(raw []byte, preferred []string) (string, string) {
	available := make(map[string]bool)
	for _, line := range strings.Split(string(raw), "\n") {
		mime := strings.TrimSpace(strings.SplitN(line, ";", 2)[0])
		available[mime] = true
	}
	for _, mime := range preferred {
		mime = strings.TrimSpace(strings.SplitN(mime, ";", 2)[0])
		if !available[mime] {
			continue
		}
		switch mime {
		case "image/png":
			return mime, "png"
		case "image/jpeg":
			return mime, "jpg"
		case "image/webp":
			return mime, "webp"
		case "image/gif":
			return mime, "gif"
		}
	}
	return "", ""
}

func sendKittyText(ctx context.Context, runner Runner, kittyCommand string, socket string, text []byte) error {
	return runner.Run(ctx, Command{
		Name:  kittyCommand,
		Args:  []string{"@", "--to", socket, "send-text", "--stdin"},
		Stdin: text,
	})
}

func RandomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
