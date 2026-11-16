package api_test

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"

	api "github.com/ogen-go/ogen/examples/ex_external_mux"
)

type petStore struct {
	mu     sync.Mutex
	nextID int64
	pets   map[int64]api.Pet
}

var _ api.Handler = (*petStore)(nil)

func newPetStore() *petStore {
	return &petStore{nextID: 1, pets: map[int64]api.Pet{}}
}

func (s *petStore) AddPet(_ context.Context, req *api.NewPet) (*api.Pet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pet := api.Pet{ID: s.nextID, Name: req.Name, Tag: req.Tag}
	s.pets[pet.ID] = pet
	s.nextID++
	return &pet, nil
}

func (s *petStore) DeletePet(_ context.Context, params api.DeletePetParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.pets, params.ID)
	return nil
}

func (s *petStore) FindPetByID(_ context.Context, params api.FindPetByIDParams) (*api.Pet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pet, ok := s.pets[params.ID]
	if !ok {
		return nil, &api.ErrorStatusCode{
			StatusCode: http.StatusNotFound,
			Response:   api.Error{Code: http.StatusNotFound, Message: "pet not found"},
		}
	}
	return &pet, nil
}

func (s *petStore) FindPets(_ context.Context, params api.FindPetsParams) ([]api.Pet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pets := make([]api.Pet, 0, len(s.pets))
	for _, id := range slices.Sorted(maps.Keys(s.pets)) {
		pet := s.pets[id]
		if len(params.Tags) > 0 && !slices.Contains(params.Tags, pet.Tag.Or("")) {
			continue
		}
		pets = append(pets, pet)
	}
	if limit, ok := params.Limit.Get(); ok {
		pets = pets[:min(int(limit), len(pets))]
	}
	return pets, nil
}

func (s *petStore) NewError(_ context.Context, err error) *api.ErrorStatusCode {
	return &api.ErrorStatusCode{
		StatusCode: http.StatusInternalServerError,
		Response:   api.Error{Code: http.StatusInternalServerError, Message: err.Error()},
	}
}

// Example mounts the generated operations on an http.ServeMux next to a
// route that is not in the OpenAPI spec.
func Example() {
	srv, err := api.NewServer(newPetStore())
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	routes := srv.Routes()
	for _, id := range slices.Sorted(maps.Keys(routes)) {
		rt := routes[id]
		pattern := rt.Method() + " " + rt.MuxPattern()
		fmt.Printf("%s: %s\n", id, pattern)
		mux.Handle(pattern, rt.Handler)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	do := func(method, path, body string) {
		req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		if err != nil {
			panic(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		fmt.Println(strings.TrimSpace(fmt.Sprintf("%s %s -> %d %s", method, path, resp.StatusCode, data)))
	}

	do(http.MethodGet, "/healthz", "")
	do(http.MethodPost, "/pets", `{"name":"Rex","tag":"dog"}`)
	do(http.MethodPost, "/pets", `{"name":"Tom","tag":"cat"}`)
	do(http.MethodGet, "/pets?tags=dog", "")
	do(http.MethodGet, "/pets/2", "")
	do(http.MethodDelete, "/pets/2", "")
	do(http.MethodGet, "/pets/2", "")
	// The generated handler validates path parameters.
	do(http.MethodGet, "/pets/abc", "")
	// http.ServeMux, not the generated code, answers unknown methods.
	do(http.MethodPut, "/pets/1", "")

	// Output:
	// addPet: POST /pets
	// deletePet: DELETE /pets/{id}
	// find pet by id: GET /pets/{id}
	// findPets: GET /pets
	// GET /healthz -> 200 ok
	// POST /pets -> 200 {"name":"Rex","tag":"dog","id":1}
	// POST /pets -> 200 {"name":"Tom","tag":"cat","id":2}
	// GET /pets?tags=dog -> 200 [{"name":"Rex","tag":"dog","id":1}]
	// GET /pets/2 -> 200 {"name":"Tom","tag":"cat","id":2}
	// DELETE /pets/2 -> 204
	// GET /pets/2 -> 404 {"code":404,"message":"pet not found"}
	// GET /pets/abc -> 400 {"error_message":"operation FindPetByID: decode params: path: \"id\": strconv.ParseInt: parsing \"abc\": invalid syntax"}
	// PUT /pets/1 -> 405 Method Not Allowed
}
