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

func TestResolveUploadModelTypeVisionHeader(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("X-Model-Type", "vision")
	if got := resolveUploadModelType(nil, req); got != "vision" {
		t.Fatalf("got %q want vision", got)
	}
}

func TestResolveUploadModelTypeVisionForm(t *testing.T) {
	body := "model_type=vision"
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if got := resolveUploadModelType(nil, req); got != "vision" {
		t.Fatalf("got %q want vision", got)
	}
}

func TestResolveUploadModelTypeFromVisionExpModel(t *testing.T) {
	body := "model=deepseek-v4-flash-vision-exp"
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	if got := resolveUploadModelType(nil, req); got != "vision" {
		t.Fatalf("got %q want vision", got)
	}
}
