package vm

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const oidcExportDir = runDir + "/oidc"

var issuerDNSLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func validateOIDCIssuer(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
		return errors.New("OIDC issuer must be an HTTPS URL without credentials, query, or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if u.Host != u.Hostname() && u.Host != u.Hostname()+":443" {
		return errors.New("OIDC issuer must use the default HTTPS port")
	}
	if len(host) > 253 || net.ParseIP(host) != nil || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errors.New("OIDC issuer requires a DNS hostname")
	}
	for _, label := range strings.Split(host, ".") {
		if !issuerDNSLabel.MatchString(label) {
			return errors.New("invalid OIDC issuer DNS hostname")
		}
	}
	return nil
}

type oidcClient struct {
	id, secret, issuer string
}

func readOIDCClient() (*oidcClient, error) {
	id := os.Getenv("LAZYCAT_AUTH_OIDC_CLIENT_ID")
	secret := os.Getenv("LAZYCAT_AUTH_OIDC_CLIENT_SECRET")
	issuer := os.Getenv("LAZYCAT_AUTH_OIDC_ISSUER_URI")
	if id == "" && secret == "" && issuer == "" {
		return nil, nil
	}
	if id == "" || secret == "" || issuer == "" {
		return nil, errors.New("incomplete LazyCat OIDC deployment variables")
	}
	if err := validateOIDCIssuer(issuer); err != nil {
		return nil, err
	}
	return &oidcClient{id: id, secret: secret, issuer: strings.TrimRight(issuer, "/")}, nil
}

// The host holds the VM lock, and starts QEMU only after this private export is complete.
func (c vmConfig) prepareOIDCExport(dir, content string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	client := c.oidc
	config, err := json.Marshal(map[string]string{
		"client_id":     client.id,
		"client_secret": client.secret,
		"discovery_url": client.issuer + "/.well-known/openid-configuration",
	})
	if err != nil {
		return err
	}
	zip, err := os.ReadFile(filepath.Join(content, "hass-oidc-auth.zip"))
	if err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{"config.json", config, 0600},
		{"hass-oidc-auth.zip", zip, 0600},
		{"hass-oidc-auth.zip.sha256", fmt.Appendf(nil, "%x\n", sha256.Sum256(zip)), 0600},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.name), f.data, f.mode); err != nil {
			return err
		}
	}
	binary, err := os.Open(filepath.Join(content, "haos-vm"))
	if err != nil {
		return err
	}
	defer binary.Close()
	guest, err := os.OpenFile(filepath.Join(dir, "haos-vm"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	_, err = io.Copy(guest, binary)
	return errors.Join(err, guest.Close())
}
