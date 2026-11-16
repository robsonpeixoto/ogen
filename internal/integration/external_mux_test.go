package integration_test

import (
	"context"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-faster/errors"
	"github.com/stretchr/testify/require"

	api "github.com/ogen-go/ogen/internal/integration/test_external_mux"
)

type externalMuxHandler struct{}

func (externalMuxHandler) GetPet(_ context.Context, params api.GetPetParams) (*api.Pet, error) {
	return &api.Pet{ID: params.PetId}, nil
}

func (externalMuxHandler) GetSecret(_ context.Context, params api.GetSecretParams) (api.GetSecretOK, error) {
	return api.GetSecretOK{Data: strings.NewReader("secret:" + params.Name)}, nil
}

func (externalMuxHandler) GetUser(_ context.Context, params api.GetUserParams) (api.GetUserOK, error) {
	return api.GetUserOK{Data: strings.NewReader("user:" + params.UserID)}, nil
}

func (externalMuxHandler) GetCode(_ context.Context, params api.GetCodeParams) (api.GetCodeOK, error) {
	return api.GetCodeOK{Data: strings.NewReader("code:" + params.R2FA)}, nil
}

func (externalMuxHandler) GetHealth(context.Context) error {
	return nil
}

func (externalMuxHandler) GetLang(_ context.Context, params api.GetLangParams) (api.GetLangOK, error) {
	return api.GetLangOK{Data: strings.NewReader("lang:" + params.Caf)}, nil
}

type externalMuxSecurity struct{}

func (externalMuxSecurity) HandleApiKey(ctx context.Context, _ api.OperationName, t api.ApiKey) (context.Context, error) {
	if t.APIKey != "good" {
		return nil, errors.New("bad key")
	}
	return ctx, nil
}

func newExternalMux(t *testing.T) (*api.Server, *httptest.Server) {
	t.Helper()

	srv, err := api.NewServer(externalMuxHandler{}, externalMuxSecurity{})
	require.NoError(t, err)

	mux := http.NewServeMux()
	for _, rt := range srv.Routes() {
		mux.Handle(rt.Method()+" "+rt.MuxPattern(), rt.Handler)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return srv, ts
}

func externalMuxGet(t *testing.T, url string, header http.Header) (int, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	require.NoError(t, err)
	maps.Copy(req.Header, header)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

func TestExternalMux(t *testing.T) {
	t.Run("Routes", func(t *testing.T) {
		srv, _ := newExternalMux(t)

		routes := srv.Routes()
		require.Len(t, routes, 6)

		pet := routes["getPet"]
		require.Equal(t, "GET", pet.Method())
		require.Equal(t, "/pets/{petId}", pet.MuxPattern())
		require.Equal(t, "/pets/{petId}", pet.PathPattern())
		require.Equal(t, "getPet", pet.OperationID())
		require.NotNil(t, pet.Handler)

		user := routes["getUser"]
		require.Equal(t, "/users/{user_id}/{$}", user.MuxPattern())
		require.Equal(t, "/users/{user-id}/", user.PathPattern())

		require.Equal(t, "/codes/{p2fa}", routes["getCode"].MuxPattern())
		require.Equal(t, "/langs/{caf_}", routes["getLang"].MuxPattern())
		require.Equal(t, "/health", routes["getHealth"].MuxPattern())
	})
	t.Run("SanitizedNames", func(t *testing.T) {
		_, ts := newExternalMux(t)

		code, body := externalMuxGet(t, ts.URL+"/users/bob/", nil)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "user:bob", body)

		code, body = externalMuxGet(t, ts.URL+"/codes/abc", nil)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "code:abc", body)

		code, body = externalMuxGet(t, ts.URL+"/langs/pt", nil)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "lang:pt", body)

		code, _ = externalMuxGet(t, ts.URL+"/health", nil)
		require.Equal(t, http.StatusOK, code)
	})
	t.Run("TrailingSlashMatchesExactly", func(t *testing.T) {
		_, ts := newExternalMux(t)

		code, _ := externalMuxGet(t, ts.URL+"/users/bob/extra", nil)
		require.Equal(t, http.StatusNotFound, code)
	})
	t.Run("RoutesReturnsCopy", func(t *testing.T) {
		srv, _ := newExternalMux(t)

		delete(srv.Routes(), "getPet")
		require.Contains(t, srv.Routes(), "getPet")
	})
	t.Run("PathValue", func(t *testing.T) {
		_, ts := newExternalMux(t)

		code, body := externalMuxGet(t, ts.URL+"/pets/42", nil)
		require.Equal(t, http.StatusOK, code)
		require.JSONEq(t, `{"id":42}`, body)
	})
	t.Run("Escaped", func(t *testing.T) {
		_, ts := newExternalMux(t)

		code, body := externalMuxGet(t, ts.URL+"/secret/a%2Fb%20c", http.Header{"X-Api-Key": {"good"}})
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "secret:a/b c", body)
	})
	t.Run("Validation", func(t *testing.T) {
		_, ts := newExternalMux(t)

		code, _ := externalMuxGet(t, ts.URL+"/pets/0", nil)
		require.Equal(t, http.StatusBadRequest, code)

		code, _ = externalMuxGet(t, ts.URL+"/pets/abc", nil)
		require.Equal(t, http.StatusBadRequest, code)
	})
	t.Run("Security", func(t *testing.T) {
		_, ts := newExternalMux(t)

		code, _ := externalMuxGet(t, ts.URL+"/secret/x", http.Header{"X-Api-Key": {"bad"}})
		require.Equal(t, http.StatusUnauthorized, code)
	})
	t.Run("FindPathHandler", func(t *testing.T) {
		srv, _ := newExternalMux(t)

		for _, tt := range []struct {
			target string
			body   string
		}{
			{"/users/bob/", "user:bob"},
			{"/secret/a%2Fb%20c", "secret:a/b c"},
		} {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, http.NoBody)
			req.Header.Set("X-Api-Key", "good")

			rt, ok := srv.FindPath(req.Method, req.URL)
			require.True(t, ok, tt.target)
			require.NotNil(t, rt.Handler, tt.target)

			rec := httptest.NewRecorder()
			rt.Handler.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, tt.target)
			require.Equal(t, tt.body, rec.Body.String(), tt.target)
		}
	})
	t.Run("RouterOwnsNotFound", func(t *testing.T) {
		_, ts := newExternalMux(t)

		code, _ := externalMuxGet(t, ts.URL+"/unknown", nil)
		require.Equal(t, http.StatusNotFound, code)
	})
}
