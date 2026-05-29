package server



import (
	"fmt"
	"net/http"
	"testing"

	"whale2api/internal/config"
	"github.com/go-chi/chi/v5"
)



func TestAPIRoutesRemainRegistered(t *testing.T) {


	t.Setenv("WHALE2API_ENV_WRITEBACK", "0")
	mem := newTestGatewayPool(t, "k1", []config.Account{{Email: "u@example.com", Password: "p"}})
	app := newTestApp(t, mem)

	routes, ok := app.Router.(chi.Routes)

	if !ok {

		t.Fatalf("app router does not expose chi routes: %T", app.Router)

	}



	got := map[string]bool{}

	if err := chi.Walk(routes, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {

		got[fmt.Sprintf("%s %s", method, route)] = true

		return nil

	}); err != nil {

		t.Fatalf("walk routes: %v", err)

	}



	for _, want := range []string{

		"GET /v1/models",

		"GET /v1/models/{model_id}",

		"POST /v1/chat/completions",

		"POST /v1/responses",

		"GET /v1/responses/{response_id}",

		"POST /v1/files",

		"GET /v1/files/{file_id}",

		"POST /v1/embeddings",

		"GET /models",

		"GET /models/{model_id}",

		"POST /chat/completions",

		"POST /responses",

		"GET /responses/{response_id}",

		"POST /files",

		"GET /files/{file_id}",

		"POST /embeddings",

	} {

		if !got[want] {

			t.Fatalf("expected route %s to be registered", want)

		}

	}



	for _, absent := range []string{

		"POST /v1/messages",

		"POST /anthropic/v1/messages",

		"POST /v1beta/models/{model}:generateContent",

	} {

		if got[absent] {

			t.Fatalf("expected route %s to be removed", absent)

		}

	}

}


