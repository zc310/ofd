package navigation

import (
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func TestURIActionsResolveBaseWithoutChangingAbsoluteTargets(t *testing.T) {
	base := "https://example.test/docs/current.ofd"
	for _, tc := range []struct{ uri, want string }{
		{"guide.html", "https://example.test/docs/guide.html"},
		{"../guide.html#part", "https://example.test/guide.html#part"},
		{"/guide?q=1", "https://example.test/guide?q=1"},
		{"https://other.test/", "https://other.test/"},
		{"mailto:user@example.test", "mailto:user@example.test"},
		{"", ""},
	} {
		action := &models.ActionURI{URI: tc.uri, Base: &base}
		if got := URI(action); got != tc.want {
			t.Errorf("URI(%q)=%q，期望%q", tc.uri, got, tc.want)
		}
		if action.URI != tc.uri || *action.Base != base {
			t.Fatal("修改了共享动作")
		}
	}
	if URI(nil) != "" {
		t.Fatal("空动作应无地址")
	}
}
