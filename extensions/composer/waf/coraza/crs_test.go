// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package coraza

import (
	"io/fs"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/corazawaf/coraza/v3"
	ctypes "github.com/corazawaf/coraza/v3/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type crsProbe struct {
	name    string
	method  string
	uri     string
	headers [][2]string
	body    string
	// blocked is whether the request must be interrupted.
	blocked bool
	// rule is the CRS rule that must match when blocked is true, and must not match otherwise.
	rule int
}

// crsDirectives are the embedded directives as documented, with the rule engine on and CRS at its
// defaults: paranoia level 1 and an inbound anomaly score threshold of 5.
var crsDirectives = strings.Join([]string{
	"Include @coraza.conf",
	"SecRuleEngine On",
	"Include @crs-setup.conf",
	"Include @owasp_crs/*.conf",
}, "\n")

// forEachCRSVersion runs f for every crs_version alias, with the files of the CRS version it
// selects.
func forEachCRSVersion(t *testing.T, f func(t *testing.T, root fs.FS)) {
	set := mustCRSBundles(t)
	for _, alias := range slices.Sorted(maps.Keys(set.aliases)) {
		b := set.aliases[alias]
		t.Run(alias+" "+b.version, func(t *testing.T) { f(t, b.fs) })
	}
}

// runCRSProbe sends one request through the request phases and returns whether it was
// interrupted, the ids of the rules that matched, and the ids of those with a severity.
func runCRSProbe(t *testing.T, waf coraza.WAF, p *crsProbe) (bool, []int, []int) {
	t.Helper()
	tx := waf.NewTransaction()
	defer func() {
		tx.ProcessLogging()
		require.NoError(t, tx.Close())
	}()
	tx.ProcessConnection("127.0.0.1", 40000, "127.0.0.1", 80)
	tx.ProcessURI(p.uri, p.method, "HTTP/1.1")
	tx.AddRequestHeader("Host", "localhost")
	tx.AddRequestHeader("User-Agent", "OWASP CRS test agent")
	tx.AddRequestHeader("Accept", "*/*")
	for _, h := range p.headers {
		tx.AddRequestHeader(h[0], h[1])
	}
	if p.body != "" {
		tx.AddRequestHeader("Content-Length", strconv.Itoa(len(p.body)))
	}

	it := tx.ProcessRequestHeaders()
	if it == nil && p.body != "" {
		var err error
		it, _, err = tx.WriteRequestBody([]byte(p.body))
		require.NoError(t, err)
	}
	if it == nil {
		var err error
		it, err = tx.ProcessRequestBody()
		require.NoError(t, err)
	}

	var ids, scored []int
	for _, mr := range tx.MatchedRules() {
		ids = append(ids, mr.Rule().ID())
		if mr.Rule().Severity() != ctypes.RuleSeverityUnset {
			scored = append(scored, mr.Rule().ID())
		}
	}
	return it != nil, ids, scored
}

// multipart joins the lines of a multipart/form-data body with CRLF.
func multipart(lines ...string) string {
	return strings.Join(lines, "\r\n") + "\r\n"
}

// Request shapes from the CRS 4.30.0 regression tests for the advisories that affect CRS 4.25.0
// under Coraza (GHSA-89h9-2j8h-9gp2, GHSA-qmx4-jfcv-fgww, GHSA-575j-qr6p-9763 and
// GHSA-6jp8-c2w2-x7wr) and the 933110 trailing-slash fix, plus controls that must behave the same
// before and after the fixes.
func TestCRSAdvisoryProbes(t *testing.T) {
	// Not text/plain: CRS does not allow it as a request content type (920420), so it would be
	// blocked whatever the charset.
	formCT := func(charset string) [][2]string {
		return [][2]string{{"Content-Type", "application/x-www-form-urlencoded; " + charset}}
	}
	multipartCT := [][2]string{{"Content-Type", "multipart/form-data; boundary=boundary"}}
	xmlCT := [][2]string{{"Content-Type", "application/xml"}}
	pathProbe := func(rule int, path string) crsProbe {
		return crsProbe{name: strconv.Itoa(rule) + " in path", method: "GET", uri: "/path/with/" + path, blocked: true, rule: rule}
	}

	probes := []crsProbe{
		// GHSA-89h9-2j8h-9gp2: the charset parameter name in upper or mixed case.
		{name: "920480 CHARSET=garbage", method: "POST", uri: "/", headers: formCT("CHARSET=garbage"), body: "test=value", blocked: true, rule: 920480},
		{name: "920480 cHarSet=utf-7", method: "POST", uri: "/", headers: formCT("cHarSet=utf-7"), body: "test=value", blocked: true, rule: 920480},

		// GHSA-qmx4-jfcv-fgww: a disallowed _charset_ hidden behind an allowed one.
		{
			name: "922100 _charset_ in query and body", method: "POST", uri: "/post?_charset_=utf-8", headers: multipartCT,
			body: multipart(
				"--boundary",
				`Content-Disposition: form-data; name="_charset_"`,
				"",
				"utf-7",
				"--boundary",
				`Content-Disposition: form-data; name="positive"`,
				"Content-Type: text/plain",
				"",
				"Let me see if I can use utf-7.",
				"--boundary--",
			),
			blocked: true, rule: 922100,
		},
		{
			name: "922100 second _charset_ part", method: "POST", uri: "/post", headers: multipartCT,
			body: multipart(
				"--boundary",
				`Content-Disposition: form-data; name="_charset_"`,
				"",
				"utf-8",
				"--boundary",
				`Content-Disposition: form-data; name="positive"`,
				"Content-Type: text/plain",
				"",
				"Try to hide the multipart with invalid charset.",
				"--boundary",
				`Content-Disposition: form-data; name="_charset_"`,
				"",
				"utf-7",
				"--boundary",
				`Content-Disposition: form-data; name="positive"`,
				"Content-Type: text/plain",
				"",
				"Let me see if I can use utf-7.",
				"--boundary--",
			),
			blocked: true, rule: 922100,
		},

		// GHSA-575j-qr6p-9763: command injection in the last path segment, paranoia level 1 rules.
		pathProbe(932130, "%24%28echo%20%24%28cat%24%28id%29%29%29"),
		pathProbe(932235, "%3Badduser%20pawnd"),
		// The advisory fixed this one in 932260; CRS 4.31.0 takes REQUEST_FILENAME out of 932260
		// (coreruleset#4842), and 932235 detects it in every version.
		pathProbe(932235, "%3Baddgroup%20pawnd"),
		pathProbe(932340, "%3Bdmesg"),
		pathProbe(932380, "view%3Dimage.jpg%26bcdboot%20c%3A%5Cwindows"),

		// 933110: a PHP file name with a trailing slash.
		{name: "933110 X-Filename a.php/", method: "GET", uri: "/upload", headers: [][2]string{{"X-Filename", "a.php/"}}, blocked: true, rule: 933110},
		{
			name: "933110 multipart filename dangerous.php/", method: "POST", uri: "/post", headers: multipartCT,
			body: multipart(
				"--boundary",
				`Content-Disposition: form-data; name="fileRap"; filename="something/dangerous.php/"`,
				"Content-Type: text/plain",
				"",
				"please block me",
				"--boundary--",
			),
			blocked: true, rule: 933110,
		},

		// GHSA-6jp8-c2w2-x7wr: payloads in XML attribute values (fixed in 4.28.0).
		{
			name: "932120 XML attribute", method: "POST", uri: "/post", headers: xmlCT,
			body:    `<?xml version="1.0"?><root><a probe="Invoke-WebRequest http://example.com/path/file.ps1"></a></root>`,
			blocked: true, rule: 932120,
		},
		{
			name: "942100 XML attribute", method: "POST", uri: "/post", headers: xmlCT,
			body:    `<?xml version="1.0"?><root><a value="' OR 1=1 --"></a></root>`,
			blocked: true, rule: 942100,
		},

		// Controls.
		{
			name: "control 932120 XML text node", method: "POST", uri: "/post", headers: xmlCT,
			body:    `<?xml version="1.0"?><root><a>Invoke-WebRequest http://example.com/path/file.ps1</a></root>`,
			blocked: true, rule: 932120,
		},
		{
			name: "control 932120 XML benign attribute", method: "POST", uri: "/post", headers: xmlCT,
			body:    `<?xml version="1.0"?><root><a probe="hello world"></a></root>`,
			blocked: false, rule: 932120,
		},
		{name: "control 920480 charset=utf-7", method: "POST", uri: "/", headers: formCT("charset=utf-7"), body: "test=value", blocked: true, rule: 920480},
		{name: "control 920480 CHARSET=utf-8", method: "POST", uri: "/", headers: formCT("CHARSET=utf-8"), body: "test=value", blocked: false, rule: 920480},
		{
			name: "control 922100 single _charset_ part utf-7", method: "POST", uri: "/post", headers: multipartCT,
			body: multipart(
				"--boundary",
				`Content-Disposition: form-data; name="_charset_"`,
				"",
				"utf-7",
				"--boundary",
				`Content-Disposition: form-data; name="positive"`,
				"Content-Type: text/plain",
				"",
				"Let me see if I can use utf-7.",
				"--boundary--",
			),
			blocked: true, rule: 922100,
		},
		{
			name: "control 922100 no _charset_ part", method: "POST", uri: "/post", headers: multipartCT,
			body: multipart(
				"--boundary",
				`Content-Disposition: form-data; name="negative"`,
				"Content-Type: text/plain",
				"",
				"utf-8 is valid but utf- should not be.",
				"--boundary--",
			),
			blocked: false, rule: 922100,
		},
		{name: "control 933110 X-Filename a.php/safe.txt", method: "GET", uri: "/", headers: [][2]string{{"X-Filename", "a.php/safe.txt"}}, blocked: false, rule: 933110},
	}

	// Every CRS version must have the fixes, the LTS release included.
	forEachCRSVersion(t, func(t *testing.T, root fs.FS) {
		waf, err := newWAF(crsDirectives, zap.NewNop(), root)
		require.NoError(t, err)
		for _, p := range probes {
			t.Run(p.name, func(t *testing.T) {
				blocked, ids, scored := runCRSProbe(t, waf, &p)
				t.Logf("blocked=%t matched rules with a severity=%v", blocked, scored)
				require.Equal(t, p.blocked, blocked, "blocked")
				require.Equal(t, p.blocked, slices.Contains(ids, p.rule), "rule %d matched", p.rule)
			})
		}
	})
}

// The setup gives phases 3 to 5 a default action with log and auditlog (CRS #4675). Without it,
// response rules still match and still block, but nothing reaches the error callback or the
// audit log.
func TestCRSResponseMatchesAreLogged(t *testing.T) {
	forEachCRSVersion(t, func(t *testing.T, root fs.FS) {
		core, logs := observer.New(zapcore.InfoLevel)
		waf, err := newWAF(crsDirectives, zap.New(core), root)
		require.NoError(t, err)

		tx := waf.NewTransaction()
		tx.ProcessConnection("127.0.0.1", 40000, "127.0.0.1", 80)
		tx.ProcessURI("/index.html", "GET", "HTTP/1.1")
		tx.AddRequestHeader("Host", "localhost")
		tx.AddRequestHeader("User-Agent", "OWASP CRS test agent")
		tx.AddRequestHeader("Accept", "*/*")
		require.Nil(t, tx.ProcessRequestHeaders())
		it, err := tx.ProcessRequestBody()
		require.NoError(t, err)
		require.Nil(t, it)

		body := "<html><body>You have an error in your SQL syntax; check the manual that corresponds to " +
			"your MySQL server version for the right syntax to use near ''' at line 1</body></html>"
		tx.AddResponseHeader("Content-Type", "text/html")
		tx.AddResponseHeader("Content-Length", strconv.Itoa(len(body)))
		require.Nil(t, tx.ProcessResponseHeaders(200, "HTTP/1.1"))
		it, _, err = tx.WriteResponseBody([]byte(body))
		require.NoError(t, err)
		if it == nil {
			it, err = tx.ProcessResponseBody()
			require.NoError(t, err)
		}
		tx.ProcessLogging()

		audited := false
		for _, mr := range tx.MatchedRules() {
			if mr.Rule().ID() == 951230 {
				a, ok := mr.(interface{ Audit() bool })
				require.True(t, ok)
				audited = a.Audit()
			}
		}
		require.NoError(t, tx.Close())

		require.NotNil(t, it)
		require.Equal(t, 959100, it.RuleID)
		entries := logs.FilterMessageSnippet(`[id "951230"]`).All()
		require.Len(t, entries, 1, "951230 (SQL error leak) reaches the error callback")
		require.Equal(t, zapcore.ErrorLevel, entries[0].Level)
		require.True(t, audited, "951230 is marked for the audit log")
	})
}
