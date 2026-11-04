package tailnet

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The keys list returns only ids (as tailscale-client-go-v2 documents), so
// FederatedIdentities must read each key to learn its type, audience and scopes.
func TestFederatedIdentitiesReadsEachKey(t *testing.T) {
	keys := map[string]string{
		"kAuth1": `{"id":"kAuth1","keyType":"auth"}`,
		"kFed1":  `{"id":"kFed1","keyType":"federated","audience":"api.tailscale.com/T1-k1","scopes":["auth_keys","devices:core"]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tskey-api-test" {
			http.Error(w, `{"message":"unauthorized"}`, 401)
			return
		}
		switch p := r.URL.Path; p {
		case "/tailnet/-/keys":
			fmt.Fprint(w, `{"keys":[{"id":"kAuth1"},{"id":"kFed1"},{"id":"kGone"}]}`)
		default:
			body, ok := keys[p[len("/tailnet/-/keys/"):]]
			if !ok {
				http.Error(w, `{"message":"not found"}`, 404)
				return
			}
			fmt.Fprint(w, body)
		}
	}))
	defer srv.Close()
	api := &API{Token: "tskey-api-test", Base: srv.URL}
	ids, err := api.FederatedIdentities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0].ID != "kFed1" || ids[0].Audience != "api.tailscale.com/T1-k1" || len(ids[0].Scopes) != 2 {
		t.Fatalf("got %+v", ids)
	}
}
