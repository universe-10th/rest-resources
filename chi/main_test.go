package chi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	chiv5 "github.com/go-chi/chi/v5"
	"github.com/universe-10th/rest-resources/memory"
	"github.com/universe-10th/rest-resources/types/services"
	"github.com/universe-10th/rest-resources/utils"
)

type testService struct {
	prefix      string
	urlArg      string
	singleton   bool
	verbs       utils.Flags[services.ResourceVerb]
	parent      services.Service
	collections []services.ExtraEndpoint
	elements    []services.ExtraEndpoint
	middlewares []services.MiddlewareFunc
}

func (s *testService) Prefix() string                             { return s.prefix }
func (s *testService) URLArg() string                             { return s.urlArg }
func (s *testService) IsSingleton() bool                          { return s.singleton }
func (s *testService) Verbs() utils.Flags[services.ResourceVerb]  { return s.verbs }
func (s *testService) CanHaveChildren() bool                      { return s.Verbs().Has(services.ResourceGet) }
func (s *testService) IsSoftDeleted() bool                        { return false }
func (s *testService) Children() []services.Service               { return nil }
func (s *testService) CollectionExtras() []services.ExtraEndpoint { return s.collections }
func (s *testService) ElementExtras() []services.ExtraEndpoint    { return s.elements }
func (s *testService) Parent() services.Service                   { return s.parent }
func (s *testService) Middlewares() []services.MiddlewareFunc     { return s.middlewares }
func (s *testService) ElementMiddleware(bool) services.MiddlewareFunc {
	return func(next services.HandlerFunc) services.HandlerFunc {
		return func(context services.Context) error {
			context.PushElement("element")
			defer context.PopElement()
			return next(context)
		}
	}
}
func (s *testService) List(context services.Context, deleted bool) error {
	endpointType, verb, name := context.CurrentEndpoint()
	middleware, _ := context.GetData("middleware")
	return context.RenderJSON(http.StatusOK, map[string]any{
		"deleted":    deleted,
		"endpoint":   endpointType,
		"verb":       verb,
		"name":       name,
		"middleware": middleware,
		"native":     context.Native() != nil,
	})
}
func (s *testService) Create(context services.Context) error {
	return context.RenderJSON(http.StatusCreated, map[string]any{"created": true})
}
func (s *testService) Get(context services.Context) error {
	_, ok := context.PeekElement(0)
	return context.RenderJSON(http.StatusOK, map[string]any{"element": ok})
}
func (s *testService) Update(context services.Context) error {
	return context.RenderNoContent(http.StatusNoContent)
}
func (s *testService) Delete(context services.Context) error {
	return context.RenderNoContent(http.StatusNoContent)
}
func (s *testService) Prune(context services.Context) error {
	return context.RenderNoContent(http.StatusNoContent)
}
func (s *testService) Restore(context services.Context) error {
	return context.RenderJSON(http.StatusOK, map[string]any{"restored": true})
}

type chiStore struct {
	memory.SoftDeletedResource[int]
	Name string `json:"name"`
}

type chiCatalog struct {
	memory.Resource[int]
	StoreID int    `json:"store_id"`
	Name    string `json:"name"`
}

type chiSetting struct {
	memory.Resource[int]
	Version string `json:"version"`
}

func TestInstallRejectsInvalidRootInputs(t *testing.T) {
	t.Parallel()

	if err := Install(nil, &testService{}); !errors.Is(err, ErrInvalidRouter) {
		t.Fatalf("expected ErrInvalidRouter, got %v", err)
	}
	router := chiv5.NewRouter()
	if err := Install(router, nil); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("expected ErrInvalidService, got %v", err)
	}
	parent := &testService{prefix: "parents"}
	child := &testService{prefix: "children", parent: parent}
	if err := Install(router, child); !errors.Is(err, ErrInvalidRootService) {
		t.Fatalf("expected ErrInvalidRootService, got %v", err)
	}
}

func TestWrapContextReturnsRequestEnvelope(t *testing.T) {
	t.Parallel()

	router := chiv5.NewRouter()
	router.Get("/items/{item_id}", func(response http.ResponseWriter, request *http.Request) {
		wrapped := WrapContext(response, request)
		pathParam, _ := wrapped.GetPathParam("item_id")
		wrapped.SetData("seen", true)
		value, ok := wrapped.GetData("seen")
		if pathParam != "42" || !ok || value != true {
			t.Fatalf("unexpected wrapped context state: path=%q ok=%v value=%#v", pathParam, ok, value)
		}
		_ = wrapped.RenderNoContent(http.StatusNoContent)
	})

	requireStatus(t, performRequest(t, router, http.MethodGet, "/items/42", nil), http.StatusNoContent)
}

func TestMustInstallPanicsForInvalidRootInputs(t *testing.T) {
	t.Parallel()

	requirePanic(t, ErrInvalidRouter, func() {
		MustInstall(nil, &testService{})
	})
	requirePanic(t, ErrInvalidService, func() {
		MustInstall(chiv5.NewRouter(), nil)
	})
	parent := &testService{prefix: "parents"}
	child := &testService{prefix: "children", parent: parent}
	requirePanic(t, ErrInvalidRootService, func() {
		MustInstall(chiv5.NewRouter(), child)
	})
}

func TestInstallWrapsMiddlewareAndContext(t *testing.T) {
	t.Parallel()

	router := chiv5.NewRouter()
	service := &testService{
		prefix: "items",
		urlArg: "item_id",
		verbs:  utils.NewFlags(services.ResourceList),
		middlewares: []services.MiddlewareFunc{
			func(next services.HandlerFunc) services.HandlerFunc {
				return func(context services.Context) error {
					context.SetData("middleware", "seen")
					return next(context)
				}
			},
		},
	}
	if err := Install(router, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	response := performRequest(t, router, http.MethodGet, "/items", nil)
	requireStatus(t, response, http.StatusOK)
	if body := response.Body.String(); !containsAll(body, `"middleware":"seen"`, `"verb":1`, `"native":true`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestInstallAcceptsChiSubrouter(t *testing.T) {
	t.Parallel()

	router := chiv5.NewRouter()
	service := &testService{
		prefix: "items",
		urlArg: "item_id",
		verbs:  utils.NewFlags(services.ResourceList),
	}
	router.Route("/api", func(r chiv5.Router) {
		if err := Install(r, service); err != nil {
			t.Fatalf("Install returned error: %v", err)
		}
	})

	requireStatus(t, performRequest(t, router, http.MethodGet, "/items", nil), http.StatusNotFound)
	requireStatus(t, performRequest(t, router, http.MethodGet, "/api/items", nil), http.StatusOK)
}

func TestContextRequestAndResponseHelpers(t *testing.T) {
	t.Parallel()

	router := chiv5.NewRouter()
	service := &testService{
		prefix: "items",
		urlArg: "item_id",
		verbs:  utils.NewFlags(services.ResourceList),
		collections: []services.ExtraEndpoint{{
			Method: http.MethodPost,
			Name:   "inspect",
			Handler: func(context services.Context) error {
				var body struct {
					Name string `json:"name"`
				}
				if err := context.BindJSON(&body); err != nil {
					return err
				}
				firstTag, _ := context.GetQueryParam("tag")
				allTags, _ := context.GetQueryParams("tag")
				header, _ := context.GetHeader("X-Test")
				headers, _ := context.GetHeaders("X-Test")
				cookie, _ := context.GetCookie("session")
				context.SetHeader("X-Result", "ok")
				context.SetCookie(services.Cookie{Name: "seen", Value: "yes", Path: "/"})
				return context.RenderJSON(http.StatusOK, map[string]any{
					"name":         body.Name,
					"first_tag":    firstTag,
					"tag_count":    len(allTags),
					"header":       header,
					"header_count": len(headers),
					"cookie":       cookie.Value,
				})
			},
		}},
	}
	if err := Install(router, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	requestBody := map[string]any{"name": "Main"}
	response := performRequestWithHeaders(t, router, http.MethodPost, "/items/inspect?tag=a&tag=b", requestBody, map[string]string{
		"X-Test": "one",
		"Cookie": "session=abc",
	})
	requireStatus(t, response, http.StatusOK)
	if response.Header().Get("X-Result") != "ok" {
		t.Fatalf("expected X-Result header, got %q", response.Header().Get("X-Result"))
	}
	if cookie := response.Header().Get("Set-Cookie"); !contains(cookie, "seen=yes") {
		t.Fatalf("expected Set-Cookie header, got %q", cookie)
	}
	if body := response.Body.String(); !containsAll(body, `"name":"Main"`, `"first_tag":"a"`, `"tag_count":2`, `"header":"one"`, `"header_count":1`, `"cookie":"abc"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestInstallCollectionExtraAndElementExtra(t *testing.T) {
	t.Parallel()

	router := chiv5.NewRouter()
	service := &testService{
		prefix: "items",
		urlArg: "item_id",
		verbs:  utils.NewFlags(services.ResourceGet, services.ResourceList),
		collections: []services.ExtraEndpoint{{
			Method: http.MethodGet,
			Name:   "search",
			Handler: func(context services.Context) error {
				endpointType, verb, name := context.CurrentEndpoint()
				_, hasElement := context.PeekElement(0)
				return context.RenderJSON(http.StatusAccepted, map[string]any{
					"endpoint": endpointType,
					"verb":     verb,
					"name":     name,
					"element":  hasElement,
				})
			},
		}},
		elements: []services.ExtraEndpoint{{
			Method: http.MethodPut,
			Name:   "publish",
			Handler: func(context services.Context) error {
				endpointType, _, name := context.CurrentEndpoint()
				_, hasElement := context.PeekElement(0)
				return context.RenderJSON(http.StatusOK, map[string]any{
					"endpoint": endpointType,
					"name":     name,
					"element":  hasElement,
				})
			},
		}},
	}
	if err := Install(router, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	collectionExtra := performRequest(t, router, http.MethodGet, "/items/search", nil)
	requireStatus(t, collectionExtra, http.StatusAccepted)
	if body := collectionExtra.Body.String(); !containsAll(body, `"endpoint":1`, `"verb":0`, `"name":"search"`, `"element":false`) {
		t.Fatalf("unexpected collection extra body: %s", body)
	}

	elementExtra := performRequest(t, router, http.MethodPut, "/items/42/publish", nil)
	requireStatus(t, elementExtra, http.StatusOK)
	if body := elementExtra.Body.String(); !containsAll(body, `"endpoint":2`, `"name":"publish"`, `"element":true`) {
		t.Fatalf("unexpected element extra body: %s", body)
	}
}

func TestInstallSingletonAndRealElementExtras(t *testing.T) {
	t.Parallel()

	router := chiv5.NewRouter()
	settingService := services.MustCreateSingletonService[int, *chiSetting]("platform", memory.NewStorage[int, *chiSetting]())
	settingService.MustAddElementExtra("GET", "statistics", func(context services.Context) error {
		elementRaw, _ := context.PeekElement(0)
		element := elementRaw.(*chiSetting)
		endpointType, _, name := context.CurrentEndpoint()
		return context.RenderJSON(http.StatusOK, map[string]any{
			"endpoint": endpointType,
			"name":     name,
			"version":  element.Version,
		})
	})

	storeService := services.MustCreateCollectionService[int, *chiStore]("stores", "store_id", memory.NewStorage[int, *chiStore]())
	storeService.MustAddElementExtra("GET", "summary", func(context services.Context) error {
		elementRaw, _ := context.PeekElement(0)
		return storeService.RenderElement(context, http.StatusOK, elementRaw.(*chiStore))
	})
	storeService.UsingElementRenderer(func(context services.Context, store *chiStore) error {
		endpointType, _, name := context.CurrentEndpoint()
		return context.RenderJSON(http.StatusOK, map[string]any{
			"endpoint": endpointType,
			"name":     name,
			"id":       store.ID,
			"store":    store.Name,
		})
	})

	if err := Install(router, settingService); err != nil {
		t.Fatalf("Install singleton returned error: %v", err)
	}
	if err := Install(router, storeService); err != nil {
		t.Fatalf("Install store returned error: %v", err)
	}

	requireStatus(t, performRequest(t, router, http.MethodPost, "/platform", map[string]any{"version": "2026.9"}), http.StatusCreated)
	stats := performRequest(t, router, http.MethodGet, "/platform/statistics", nil)
	requireStatus(t, stats, http.StatusOK)
	if body := stats.Body.String(); !containsAll(body, `"endpoint":2`, `"name":"statistics"`, `"version":"2026.9"`) {
		t.Fatalf("unexpected singleton extra body: %s", body)
	}

	createStore := performRequest(t, router, http.MethodPost, "/stores", map[string]any{"name": "Main"})
	requireStatus(t, createStore, http.StatusOK)
	var store chiStore
	decodeJSON(t, createStore, &store)

	summary := performRequest(t, router, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/summary", nil)
	requireStatus(t, summary, http.StatusOK)
	if body := summary.Body.String(); !containsAll(body, `"endpoint":2`, `"name":"summary"`, `"store":"Main"`) {
		t.Fatalf("unexpected element extra body: %s", body)
	}

	requireStatus(t, performRequest(t, router, http.MethodDelete, "/stores/"+strconv.Itoa(store.ID), nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, router, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/summary", nil), http.StatusNotFound)
}

func TestInstallNestedAndDeletedRoutes(t *testing.T) {
	t.Parallel()

	router := chiv5.NewRouter()
	storeService := services.MustCreateCollectionService[int, *chiStore]("stores", "store_id", memory.NewStorage[int, *chiStore]())
	catalogService := services.MustCreateCollectionService[int, *chiCatalog]("catalogs", "catalog_id", memory.NewStorage[int, *chiCatalog]())
	catalogService.MustAttachTo(storeService, "store_id")
	if err := Install(router, storeService); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	storeResponse := performRequest(t, router, http.MethodPost, "/stores", map[string]any{"name": "Main"})
	requireStatus(t, storeResponse, http.StatusCreated)
	var store chiStore
	decodeJSON(t, storeResponse, &store)

	catalogResponse := performRequest(t, router, http.MethodPost, "/stores/"+strconv.Itoa(store.ID)+"/catalogs", map[string]any{"name": "Fall"})
	requireStatus(t, catalogResponse, http.StatusCreated)
	var catalog chiCatalog
	decodeJSON(t, catalogResponse, &catalog)
	if catalog.StoreID != store.ID {
		t.Fatalf("expected child constraint %d, got %#v", store.ID, catalog)
	}

	requireStatus(t, performRequest(t, router, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/catalogs", nil), http.StatusOK)
	requireStatus(t, performRequest(t, router, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/catalogs/"+strconv.Itoa(catalog.ID), nil), http.StatusOK)
	requireStatus(t, performRequest(t, router, http.MethodDelete, "/stores/"+strconv.Itoa(store.ID), nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, router, http.MethodGet, "/stores/deleted", nil), http.StatusOK)
	requireStatus(t, performRequest(t, router, http.MethodGet, "/stores/deleted/"+strconv.Itoa(store.ID), nil), http.StatusOK)
	requireStatus(t, performRequest(t, router, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/catalogs", nil), http.StatusNotFound)
	requireStatus(t, performRequest(t, router, http.MethodPost, "/stores/deleted/"+strconv.Itoa(store.ID), nil), http.StatusOK)
	requireStatus(t, performRequest(t, router, http.MethodDelete, "/stores/"+strconv.Itoa(store.ID), nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, router, http.MethodDelete, "/stores/deleted/"+strconv.Itoa(store.ID), nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, router, http.MethodGet, "/stores/deleted/"+strconv.Itoa(store.ID), nil), http.StatusNotFound)
}

func performRequest(t *testing.T, handler http.Handler, method string, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return performRequestWithHeaders(t, handler, method, target, body, nil)
}

func performRequestWithHeaders(t *testing.T, handler http.Handler, method string, target string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	var requestBody *bytes.Reader
	if body == nil {
		requestBody = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal returned error: %v", err)
		}
		requestBody = bytes.NewReader(encoded)
	}

	request := httptest.NewRequest(method, target, requestBody)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Add(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func requireStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("expected status %d, got %d with body %s", status, response.Code, response.Body.String())
	}
}

func decodeJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("json decode returned error: %v with body %s", err, response.Body.String())
	}
}

func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !contains(value, fragment) {
			return false
		}
	}
	return true
}

func contains(value string, fragment string) bool {
	for start := 0; start+len(fragment) <= len(value); start++ {
		if value[start:start+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

func requirePanic(t *testing.T, expected error, fn func()) {
	t.Helper()
	defer func() {
		value := recover()
		if value == nil {
			t.Fatalf("expected panic %v", expected)
		}
		err, ok := value.(error)
		if !ok || !errors.Is(err, expected) {
			t.Fatalf("expected panic %v, got %#v", expected, value)
		}
	}()
	fn()
}
