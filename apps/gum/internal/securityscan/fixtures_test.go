package securityscan

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// patterns enumerates secret shapes a testdata fixture must never embed.
type secretPattern struct {
	name string
	re   *regexp.Regexp
}

var secretPatterns = []secretPattern{
	{"google_api_key", regexp.MustCompile(`AIza[0-9A-Za-z_\-]{35}`)},
	{"bearer_token", regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9_\-.~+/=]{20,}`)},
	{"private_key_pem", regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA |)PRIVATE KEY-----`)},
	{"oauth_refresh_token", regexp.MustCompile(`1//0[A-Za-z0-9_\-]{40,}`)},
	{"aws_access_key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
}

// allowedEmailDomainSuffixes covers RFC 2606 reserved test domains plus
// internal placeholders. Both the exact domain and any subdomain match.
var allowedEmailDomainSuffixes = []string{
	"example.com",
	"example.org",
	"example.net",
	"test.local",
	"localhost",
	"redacted.local",
	"placeholder.dev",
}

// iCalUIDLikeRe matches values that look like Google Calendar iCalUIDs
// (synthetic test event IDs of the form evNNN@google.com), not user emails.
var iCalUIDLikeRe = regexp.MustCompile(`^ev\d+@google\.com$`)

var emailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// allowedLiteralExceptions covers tokens that look like secrets but are
// documented placeholders, schema literals, or canonical test vectors.
var allowedLiteralExceptions = []string{
	"AIzaSyExampleKeyPlaceholderForFixtureDocs",
	"AIzaSy0000000000000000000000000000000000",
}

func TestFixturesNoSecrets(t *testing.T) {
	moduleRoot := moduleRoot(t)

	var offenders []string
	err := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !insideTestdata(path) {
			return nil
		}
		if isBinaryFixture(path) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(moduleRoot, path)
		for _, p := range secretPatterns {
			for _, match := range p.re.FindAll(data, -1) {
				if isAllowedLiteral(string(match)) {
					continue
				}
				offenders = append(offenders, rel+": "+p.name+": "+string(match))
			}
		}
		for _, match := range emailRe.FindAll(data, -1) {
			email := string(match)
			if iCalUIDLikeRe.MatchString(email) {
				continue
			}
			at := strings.LastIndex(email, "@")
			if at < 0 {
				continue
			}
			domain := strings.ToLower(email[at+1:])
			if emailDomainAllowed(domain) {
				continue
			}
			offenders = append(offenders, rel+": email_non_example: "+email)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("found %d possible secret(s) in testdata:\n  %s", len(offenders), strings.Join(offenders, "\n  "))
	}
}

func isAllowedLiteral(s string) bool {
	for _, allow := range allowedLiteralExceptions {
		if s == allow {
			return true
		}
	}
	return false
}

func emailDomainAllowed(domain string) bool {
	for _, suffix := range allowedEmailDomainSuffixes {
		if domain == suffix || strings.HasSuffix(domain, "."+suffix) {
			return true
		}
	}
	return false
}

func insideTestdata(p string) bool {
	parts := strings.Split(filepath.ToSlash(p), "/")
	for _, part := range parts {
		if part == "testdata" {
			return true
		}
	}
	return false
}

func isBinaryFixture(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".bin", ".gz", ".zip", ".png", ".jpg", ".jpeg", ".pdf", ".exe":
		return true
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	if len(data) == 0 {
		return false
	}
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	return bytes.IndexByte(head, 0) >= 0
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found from %s", wd)
		}
		dir = parent
	}
}
