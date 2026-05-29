package files

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveUploadModelTypeExpertHeaderMapsToDefault(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("X-Model-Type", "expert")
	if got := resolveUploadModelType(nil, req); got != "default" {
		t.Fatalf("got %q want default", got)
	}
}

func TestResolveUploadModelTypeExpertFormMapsToDefault(t *testing.T) {
	body := "model_type=expert"
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if got := resolveUploadModelType(nil, req); got != "default" {
		t.Fatalf("got %q want default", got)
	}
}

func TestResolveUploadModelTypeMistakenModelIDInModelTypeField(t *testing.T) {
	body := "model_type=deepseek-v4-pro"
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if got := resolveUploadModelType(nil, req); got != "default" {
		t.Fatalf("got %q want default", got)
	}
}
