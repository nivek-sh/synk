package cli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type updateTransport map[string]string

func (transport updateTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, ok := transport[request.URL.Path]
	if !ok {
		return nil, fmt.Errorf("unexpected download %s", request.URL)
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestDownloadSynkUpdateVerifiesChecksum(t *testing.T) {
	const asset = "synk_0.4.3_amd64.deb"
	const packageData = "package bytes"
	hash := sha256.Sum256([]byte(packageData))
	manifest := fmt.Sprintf("%x  %s\n", hash, asset)
	for _, test := range []struct {
		name string
		body string
		sums string
		want string
	}{
		{name: "valid", body: packageData, sums: manifest},
		{name: "corrupt", body: "corrupted package", sums: manifest, want: "checksum verification failed"},
		{name: "missing", sums: strings.ReplaceAll(manifest, asset, "other.deb"), want: "no valid checksum found"},
		{name: "duplicate", sums: manifest + manifest, want: "duplicate checksum"},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport := updateTransport{"/SHA256SUMS": test.sums, "/" + asset: test.body}
			destination := filepath.Join(t.TempDir(), asset)
			err := downloadSynkUpdate(context.Background(), &http.Client{Transport: transport}, "https://example.test", asset, destination)
			if test.want != "" {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("download error = %v, want %q", err, test.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(destination)
			if err != nil || string(data) != packageData {
				t.Fatalf("downloaded = %q, error = %v", data, err)
			}
		})
	}
}

func TestNewerRelease(t *testing.T) {
	for _, test := range []struct {
		latest, current string
		want            bool
	}{
		{"0.4.10", "0.4.9", true},
		{"0.4.2", "0.4.2", false},
		{"0.4.9", "0.4.10", false},
		{"1.0.0", "0.99.99", true},
	} {
		if got := newerRelease(test.latest, test.current); got != test.want {
			t.Errorf("newerRelease(%q, %q) = %t, want %t", test.latest, test.current, got, test.want)
		}
	}
}
