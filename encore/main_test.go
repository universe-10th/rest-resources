package encore

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/universe-10th/rest-resources/memory"
	"github.com/universe-10th/rest-resources/types/services"
)

type encoreStore struct {
	memory.SoftDeletedResource[int]
	Name string `json:"name"`
}

type encoreCatalog struct {
	memory.Resource[int]
	StoreID int    `json:"store_id"`
	Name    string `json:"name"`
}

type encoreSetting struct {
	memory.Resource[int]
	Version string `json:"version"`
}

type panicParentService struct {
	services.Service
}

func (s panicParentService) Parent() services.Service {
	panic("boom")
}

func TestNewHandlerRejectsInvalidServices(t *testing.T) {
	t.Parallel()

	if _, err := NewHandler(nil); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("expected ErrInvalidService, got %v", err)
	}

	parent := services.MustCreateCollectionService[int, *encoreStore]("parents", "parent_id", memory.NewStorage[int, *encoreStore]())
	child := services.MustCreateCollectionService[int, *encoreStore]("children", "child_id", memory.NewStorage[int, *encoreStore]())
	child.MustAttachTo(parent, "id")
	if _, err := NewHandler(child); !errors.Is(err, ErrInvalidRootService) {
		t.Fatalf("expected ErrInvalidRootService, got %v", err)
	}
}

func TestNewHandlerRethrowsNonErrorPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		value := recover()
		if value != "boom" {
			t.Fatalf("expected boom panic, got %#v", value)
		}
	}()
	_, _ = NewHandler(panicParentService{Service: services.MustCreateCollectionService[int, *encoreStore]("stores", "store_id", memory.NewStorage[int, *encoreStore]())})
}

func TestWrapContextReturnsRequestEnvelope(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		wrapped := WrapContext(response, request)
		wrapped.SetData("seen", true)
		value, ok := wrapped.GetData("seen")
		if !ok || value != true || wrapped.Native() == nil {
			t.Fatalf("unexpected wrapped context state: ok=%v value=%#v native=%#v", ok, value, wrapped.Native())
		}
		_ = wrapped.RenderNoContent(http.StatusNoContent)
	})

	requireStatus(t, performRequest(t, handler, http.MethodGet, "/items", nil), http.StatusNoContent)
}

func TestMustNewHandlerPanicsForInvalidServices(t *testing.T) {
	t.Parallel()

	requirePanic(t, ErrInvalidService, func() {
		MustNewHandler(nil)
	})

	parent := services.MustCreateCollectionService[int, *encoreStore]("parents", "parent_id", memory.NewStorage[int, *encoreStore]())
	child := services.MustCreateCollectionService[int, *encoreStore]("children", "child_id", memory.NewStorage[int, *encoreStore]())
	child.MustAttachTo(parent, "id")
	requirePanic(t, ErrInvalidRootService, func() {
		MustNewHandler(child)
	})
}

func TestContextRequestAndResponseHelpers(t *testing.T) {
	t.Parallel()

	storeService := services.MustCreateCollectionService[int, *encoreStore]("stores", "store_id", memory.NewStorage[int, *encoreStore]())
	storeService.MustAddCollectionExtra("POST", "inspect", func(context services.Context) error {
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
			"native":       context.Native() != nil,
		})
	})
	handler := MustNewHandler(storeService)

	response := performRequestWithHeaders(t, handler, http.MethodPost, "/stores/inspect?tag=a&tag=b", map[string]any{"name": "Main"}, map[string]string{
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
	if body := response.Body.String(); !containsAll(body, `"name":"Main"`, `"first_tag":"a"`, `"tag_count":2`, `"header":"one"`, `"header_count":1`, `"cookie":"abc"`, `"native":true`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestNewHandlerSupportsEncoreRawEndpointRouting(t *testing.T) {
	t.Parallel()

	settingService := services.MustCreateSingletonService[int, *encoreSetting]("platform", memory.NewStorage[int, *encoreSetting]())
	settingService.MustAddElementExtra("GET", "statistics", func(context services.Context) error {
		elementRaw, _ := context.PeekElement(0)
		element := elementRaw.(*encoreSetting)
		endpointType, _, name := context.CurrentEndpoint()
		return context.RenderJSON(http.StatusOK, map[string]any{
			"endpoint": endpointType,
			"name":     name,
			"version":  element.Version,
		})
	})

	storeService := services.MustCreateCollectionService[int, *encoreStore]("stores", "store_id", memory.NewStorage[int, *encoreStore]())
	storeService.MustAddCollectionExtra("GET", "search", func(context services.Context) error {
		endpointType, _, name := context.CurrentEndpoint()
		_, hasElement := context.PeekElement(0)
		return context.RenderJSON(http.StatusAccepted, map[string]any{
			"endpoint": endpointType,
			"name":     name,
			"element":  hasElement,
		})
	})
	storeService.MustAddElementExtra("GET", "summary", func(context services.Context) error {
		elementRaw, _ := context.PeekElement(0)
		return storeService.RenderElement(context, http.StatusOK, elementRaw.(*encoreStore))
	})
	storeService.UsingElementRenderer(func(context services.Context, store *encoreStore) error {
		endpointType, _, name := context.CurrentEndpoint()
		return context.RenderJSON(http.StatusOK, map[string]any{
			"endpoint": endpointType,
			"name":     name,
			"id":       store.ID,
			"store":    store.Name,
		})
	})

	handler, err := NewHandler(settingService, storeService)
	if err != nil {
		t.Fatalf("NewHandler returned error: %v", err)
	}

	requireStatus(t, performRequest(t, handler, http.MethodPost, "/platform", map[string]any{"version": "2026.9"}), http.StatusCreated)
	stats := performRequest(t, handler, http.MethodGet, "/platform/statistics", nil)
	requireStatus(t, stats, http.StatusOK)
	if body := stats.Body.String(); !containsAll(body, `"endpoint":2`, `"name":"statistics"`, `"version":"2026.9"`) {
		t.Fatalf("unexpected singleton extra body: %s", body)
	}

	search := performRequest(t, handler, http.MethodGet, "/stores/search", nil)
	requireStatus(t, search, http.StatusAccepted)
	if body := search.Body.String(); !containsAll(body, `"endpoint":1`, `"name":"search"`, `"element":false`) {
		t.Fatalf("unexpected collection extra body: %s", body)
	}

	createStore := performRequest(t, handler, http.MethodPost, "/stores", map[string]any{"name": "Main"})
	requireStatus(t, createStore, http.StatusOK)
	var store encoreStore
	decodeJSON(t, createStore, &store)

	summary := performRequest(t, handler, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/summary", nil)
	requireStatus(t, summary, http.StatusOK)
	if body := summary.Body.String(); !containsAll(body, `"endpoint":2`, `"name":"summary"`, `"store":"Main"`) {
		t.Fatalf("unexpected element extra body: %s", body)
	}

	requireStatus(t, performRequest(t, handler, http.MethodDelete, "/stores/"+strconv.Itoa(store.ID), nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, handler, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/summary", nil), http.StatusNotFound)
}

func TestNewHandlerSupportsNestedAndDeletedRoutes(t *testing.T) {
	t.Parallel()

	storeService := services.MustCreateCollectionService[int, *encoreStore]("stores", "store_id", memory.NewStorage[int, *encoreStore]())
	catalogService := services.MustCreateCollectionService[int, *encoreCatalog]("catalogs", "catalog_id", memory.NewStorage[int, *encoreCatalog]())
	catalogService.MustAttachTo(storeService, "store_id")
	handler := MustNewHandler(storeService)

	storeResponse := performRequest(t, handler, http.MethodPost, "/stores", map[string]any{"name": "Main"})
	requireStatus(t, storeResponse, http.StatusCreated)
	var store encoreStore
	decodeJSON(t, storeResponse, &store)

	catalogResponse := performRequest(t, handler, http.MethodPost, "/stores/"+strconv.Itoa(store.ID)+"/catalogs", map[string]any{"name": "Fall"})
	requireStatus(t, catalogResponse, http.StatusCreated)
	var catalog encoreCatalog
	decodeJSON(t, catalogResponse, &catalog)
	if catalog.StoreID != store.ID {
		t.Fatalf("expected child constraint %d, got %#v", store.ID, catalog)
	}

	requireStatus(t, performRequest(t, handler, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/catalogs", nil), http.StatusOK)
	requireStatus(t, performRequest(t, handler, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/catalogs/"+strconv.Itoa(catalog.ID), nil), http.StatusOK)
	requireStatus(t, performRequest(t, handler, http.MethodDelete, "/stores/"+strconv.Itoa(store.ID), nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, handler, http.MethodGet, "/stores/deleted", nil), http.StatusOK)
	requireStatus(t, performRequest(t, handler, http.MethodGet, "/stores/deleted/"+strconv.Itoa(store.ID), nil), http.StatusOK)
	requireStatus(t, performRequest(t, handler, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/catalogs", nil), http.StatusNotFound)
	requireStatus(t, performRequest(t, handler, http.MethodPost, "/stores/deleted/"+strconv.Itoa(store.ID), nil), http.StatusOK)
	requireStatus(t, performRequest(t, handler, http.MethodDelete, "/stores/"+strconv.Itoa(store.ID), nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, handler, http.MethodDelete, "/stores/deleted/"+strconv.Itoa(store.ID), nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, handler, http.MethodGet, "/stores/deleted/"+strconv.Itoa(store.ID), nil), http.StatusNotFound)
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
