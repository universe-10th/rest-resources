package fiber

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"testing"

	fiberv3 "github.com/gofiber/fiber/v3"
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

type fiberStore struct {
	memory.SoftDeletedResource[int]
	Name string `json:"name"`
}

type fiberCatalog struct {
	memory.Resource[int]
	StoreID int    `json:"store_id"`
	Name    string `json:"name"`
}

type fiberSetting struct {
	memory.Resource[int]
	Version string `json:"version"`
}

type fiberSoftSetting struct {
	memory.SoftDeletedResource[int]
	Version string `json:"version"`
}

type fiberStoreProfile struct {
	memory.Resource[int]
	StoreID int    `json:"store_id"`
	Note    string `json:"note"`
}

type panicParentService struct {
	*testService
}

func (s panicParentService) Parent() services.Service {
	panic("boom")
}

type fakeRouter struct{}

func (fakeRouter) Add([]string, string, any, ...any) fiberv3.Router {
	panic("unexpected Add")
}

func (fakeRouter) Group(string, ...any) fiberv3.Router {
	panic("unexpected Group")
}

func TestInstallRejectsInvalidRootInputs(t *testing.T) {
	t.Parallel()

	if err := Install(nil, &testService{}); !errors.Is(err, ErrInvalidFiberApp) {
		t.Fatalf("expected ErrInvalidFiberApp, got %v", err)
	}
	app := fiberv3.New()
	if err := Install(app, nil); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("expected ErrInvalidService, got %v", err)
	}
	parent := &testService{prefix: "parents"}
	child := &testService{prefix: "children", parent: parent}
	if err := Install(app, child); !errors.Is(err, ErrInvalidRootService) {
		t.Fatalf("expected ErrInvalidRootService, got %v", err)
	}
}

func TestInstallRethrowsNonErrorPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		value := recover()
		if value != "boom" {
			t.Fatalf("expected boom panic, got %#v", value)
		}
	}()
	_ = Install(fiberv3.New(), panicParentService{testService: &testService{prefix: "items"}})
}

func TestIsNilRouterWithValueRouter(t *testing.T) {
	t.Parallel()

	if isNilRouter(fakeRouter{}) {
		t.Fatal("expected value router not to be nil")
	}
}

func TestMustInstallPanicsForInvalidRootInputs(t *testing.T) {
	t.Parallel()

	requirePanic(t, ErrInvalidFiberApp, func() {
		MustInstall(nil, &testService{})
	})
	requirePanic(t, ErrInvalidService, func() {
		MustInstall(fiberv3.New(), nil)
	})
	parent := &testService{prefix: "parents"}
	child := &testService{prefix: "children", parent: parent}
	requirePanic(t, ErrInvalidRootService, func() {
		MustInstall(fiberv3.New(), child)
	})
}

func TestInstallWrapsMiddlewareAndContext(t *testing.T) {
	t.Parallel()

	app := fiberv3.New()
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
	if err := Install(app, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	response := performRequest(t, app, http.MethodGet, "/items", nil)
	requireStatus(t, response, http.StatusOK)
	if body := readBody(t, response); !containsAll(body, `"middleware":"seen"`, `"verb":1`, `"native":true`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestInstallAcceptsFiberGroup(t *testing.T) {
	t.Parallel()

	app := fiberv3.New()
	group := app.Group("/api")
	service := &testService{
		prefix: "items",
		urlArg: "item_id",
		verbs:  utils.NewFlags(services.ResourceList),
	}
	if err := Install(group, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	requireStatus(t, performRequest(t, app, http.MethodGet, "/items", nil), http.StatusNotFound)
	requireStatus(t, performRequest(t, app, http.MethodGet, "/api/items", nil), http.StatusOK)
}

func TestContextRequestAndResponseHelpers(t *testing.T) {
	t.Parallel()

	app := fiberv3.New()
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
	if err := Install(app, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	response := performRequestWithHeaders(t, app, http.MethodPost, "/items/inspect?tag=a&tag=b", map[string]any{"name": "Main"}, map[string]string{
		"X-Test": "one",
		"Cookie": "session=abc",
	})
	requireStatus(t, response, http.StatusOK)
	if response.Header.Get("X-Result") != "ok" {
		t.Fatalf("expected X-Result header, got %q", response.Header.Get("X-Result"))
	}
	if cookie := response.Header.Get("Set-Cookie"); !contains(cookie, "seen=yes") {
		t.Fatalf("expected Set-Cookie header, got %q", cookie)
	}
	if body := readBody(t, response); !containsAll(body, `"name":"Main"`, `"first_tag":"a"`, `"tag_count":2`, `"header":"one"`, `"header_count":1`, `"cookie":"abc"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestContextMissingRequestHelpers(t *testing.T) {
	t.Parallel()

	app := fiberv3.New()
	service := &testService{
		prefix: "items",
		urlArg: "item_id",
		verbs:  utils.NewFlags(services.ResourceList),
		collections: []services.ExtraEndpoint{{
			Method: http.MethodGet,
			Name:   "inspect",
			Handler: func(context services.Context) error {
				results := map[string]bool{}
				_, err := context.GetPathParam("missing")
				results["path"] = err == nil
				_, err = context.GetQueryParam("missing")
				results["query"] = err == nil
				_, err = context.GetQueryParams("missing")
				results["queries"] = err == nil
				_, err = context.GetHeader("missing")
				results["header"] = err == nil
				_, err = context.GetHeaders("missing")
				results["headers"] = err == nil
				_, err = context.GetCookie("missing")
				results["cookie"] = err == nil
				_, ok := context.PopElement()
				results["pop"] = ok
				return context.RenderJSON(http.StatusOK, results)
			},
		}},
	}
	if err := Install(app, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	response := performRequest(t, app, http.MethodGet, "/items/inspect", nil)
	requireStatus(t, response, http.StatusOK)
	if body := readBody(t, response); !containsAll(body, `"path":false`, `"query":false`, `"queries":false`, `"header":false`, `"headers":false`, `"cookie":false`, `"pop":false`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestFiberSameSiteMapping(t *testing.T) {
	t.Parallel()

	if toFiberSameSite(services.CookieSameSiteLax) != "Lax" {
		t.Fatal("expected Lax same-site mapping")
	}
	if toFiberSameSite(services.CookieSameSiteStrict) != "Strict" {
		t.Fatal("expected Strict same-site mapping")
	}
	if toFiberSameSite(services.CookieSameSiteNone) != "None" {
		t.Fatal("expected None same-site mapping")
	}
	if toFiberSameSite(services.CookieSameSiteDefault) != "" {
		t.Fatal("expected default same-site mapping")
	}
}

func TestContextResponseAlreadySent(t *testing.T) {
	t.Parallel()

	app := fiberv3.New()
	app.Get("/", func(native fiberv3.Ctx) error {
		context := WrapContext(native)
		if context.ResponseAlreadySent() {
			t.Fatal("expected response not to be sent before rendering")
		}
		if err := context.RenderNoContent(http.StatusNoContent); err != nil {
			return err
		}
		if !context.ResponseAlreadySent() {
			t.Fatal("expected response to be sent after rendering")
		}
		return nil
	})

	requireStatus(t, performRequest(t, app, http.MethodGet, "/", nil), http.StatusNoContent)
}

func TestInstallCollectionExtraAndElementExtra(t *testing.T) {
	t.Parallel()

	app := fiberv3.New()
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
	if err := Install(app, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	collectionExtra := performRequest(t, app, http.MethodGet, "/items/search", nil)
	requireStatus(t, collectionExtra, http.StatusAccepted)
	if body := readBody(t, collectionExtra); !containsAll(body, `"endpoint":1`, `"verb":0`, `"name":"search"`, `"element":false`) {
		t.Fatalf("unexpected collection extra body: %s", body)
	}

	elementExtra := performRequest(t, app, http.MethodPut, "/items/42/publish", nil)
	requireStatus(t, elementExtra, http.StatusOK)
	if body := readBody(t, elementExtra); !containsAll(body, `"endpoint":2`, `"name":"publish"`, `"element":true`) {
		t.Fatalf("unexpected element extra body: %s", body)
	}
}

func TestInstallSingletonAndRealElementExtras(t *testing.T) {
	t.Parallel()

	app := fiberv3.New()
	settingService := services.MustCreateSingletonService[int, *fiberSetting]("platform", memory.NewStorage[int, *fiberSetting]())
	settingService.MustAddElementExtra("GET", "statistics", func(context services.Context) error {
		elementRaw, _ := context.PeekElement(0)
		element := elementRaw.(*fiberSetting)
		endpointType, _, name := context.CurrentEndpoint()
		return context.RenderJSON(http.StatusOK, map[string]any{
			"endpoint": endpointType,
			"name":     name,
			"version":  element.Version,
		})
	})

	storeService := services.MustCreateCollectionService[int, *fiberStore]("stores", "store_id", memory.NewStorage[int, *fiberStore]())
	storeService.MustAddElementExtra("GET", "summary", func(context services.Context) error {
		elementRaw, _ := context.PeekElement(0)
		return storeService.RenderElement(context, http.StatusOK, elementRaw.(*fiberStore))
	})
	storeService.UsingElementRenderer(func(context services.Context, store *fiberStore) error {
		endpointType, _, name := context.CurrentEndpoint()
		return context.RenderJSON(http.StatusOK, map[string]any{
			"endpoint": endpointType,
			"name":     name,
			"id":       store.ID,
			"store":    store.Name,
		})
	})

	if err := Install(app, settingService); err != nil {
		t.Fatalf("Install singleton returned error: %v", err)
	}
	if err := Install(app, storeService); err != nil {
		t.Fatalf("Install store returned error: %v", err)
	}

	requireStatus(t, performRequest(t, app, http.MethodPost, "/platform", map[string]any{"version": "2026.9"}), http.StatusCreated)
	stats := performRequest(t, app, http.MethodGet, "/platform/statistics", nil)
	requireStatus(t, stats, http.StatusOK)
	if body := readBody(t, stats); !containsAll(body, `"endpoint":2`, `"name":"statistics"`, `"version":"2026.9"`) {
		t.Fatalf("unexpected singleton extra body: %s", body)
	}

	createStore := performRequest(t, app, http.MethodPost, "/stores", map[string]any{"name": "Main"})
	requireStatus(t, createStore, http.StatusOK)
	var store fiberStore
	decodeJSON(t, createStore, &store)

	summary := performRequest(t, app, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/summary", nil)
	requireStatus(t, summary, http.StatusOK)
	if body := readBody(t, summary); !containsAll(body, `"endpoint":2`, `"name":"summary"`, `"store":"Main"`) {
		t.Fatalf("unexpected element extra body: %s", body)
	}

	requireStatus(t, performRequest(t, app, http.MethodDelete, "/stores/"+strconv.Itoa(store.ID), nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, app, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/summary", nil), http.StatusNotFound)
}

func TestInstallNestedAndDeletedRoutes(t *testing.T) {
	t.Parallel()

	app := fiberv3.New()
	storeService := services.MustCreateCollectionService[int, *fiberStore]("stores", "store_id", memory.NewStorage[int, *fiberStore]())
	catalogService := services.MustCreateCollectionService[int, *fiberCatalog]("catalogs", "catalog_id", memory.NewStorage[int, *fiberCatalog]())
	catalogService.MustAttachTo(storeService, "store_id")
	profileService := services.MustCreateSingletonService[int, *fiberStoreProfile]("profile", memory.NewStorage[int, *fiberStoreProfile]())
	profileService.MustAttachTo(storeService, "store_id")
	if err := Install(app, storeService); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	storeResponse := performRequest(t, app, http.MethodPost, "/stores", map[string]any{"name": "Main"})
	requireStatus(t, storeResponse, http.StatusCreated)
	var store fiberStore
	decodeJSON(t, storeResponse, &store)

	catalogResponse := performRequest(t, app, http.MethodPost, "/stores/"+strconv.Itoa(store.ID)+"/catalogs", map[string]any{"name": "Fall"})
	requireStatus(t, catalogResponse, http.StatusCreated)
	var catalog fiberCatalog
	decodeJSON(t, catalogResponse, &catalog)
	if catalog.StoreID != store.ID {
		t.Fatalf("expected child constraint %d, got %#v", store.ID, catalog)
	}

	profileResponse := performRequest(t, app, http.MethodPost, "/stores/"+strconv.Itoa(store.ID)+"/profile", map[string]any{"note": "Scoped"})
	requireStatus(t, profileResponse, http.StatusCreated)
	var profile fiberStoreProfile
	decodeJSON(t, profileResponse, &profile)
	if profile.StoreID != store.ID {
		t.Fatalf("expected profile constraint %d, got %#v", store.ID, profile)
	}

	requireStatus(t, performRequest(t, app, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/catalogs", nil), http.StatusOK)
	requireStatus(t, performRequest(t, app, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/catalogs/"+strconv.Itoa(catalog.ID), nil), http.StatusOK)
	requireStatus(t, performRequest(t, app, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/profile", nil), http.StatusOK)
	requireStatus(t, performRequest(t, app, http.MethodDelete, "/stores/"+strconv.Itoa(store.ID), nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, app, http.MethodGet, "/stores/deleted", nil), http.StatusOK)
	requireStatus(t, performRequest(t, app, http.MethodGet, "/stores/deleted/"+strconv.Itoa(store.ID), nil), http.StatusOK)
	requireStatus(t, performRequest(t, app, http.MethodGet, "/stores/"+strconv.Itoa(store.ID)+"/catalogs", nil), http.StatusNotFound)
	requireStatus(t, performRequest(t, app, http.MethodPost, "/stores/deleted/"+strconv.Itoa(store.ID), nil), http.StatusOK)
	requireStatus(t, performRequest(t, app, http.MethodDelete, "/stores/"+strconv.Itoa(store.ID), nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, app, http.MethodDelete, "/stores/deleted/"+strconv.Itoa(store.ID), nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, app, http.MethodGet, "/stores/deleted/"+strconv.Itoa(store.ID), nil), http.StatusNotFound)
}

func TestInstallSoftDeletedSingletonRoutes(t *testing.T) {
	t.Parallel()

	app := fiberv3.New()
	service := services.MustCreateSingletonService[int, *fiberSoftSetting]("platform", memory.NewStorage[int, *fiberSoftSetting]())
	if err := Install(app, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	requireStatus(t, performRequest(t, app, http.MethodPost, "/platform", map[string]any{"version": "2026.9"}), http.StatusCreated)
	requireStatus(t, performRequest(t, app, http.MethodDelete, "/platform", nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, app, http.MethodGet, "/platform/deleted", nil), http.StatusOK)
	requireStatus(t, performRequest(t, app, http.MethodPost, "/platform/deleted", nil), http.StatusOK)
	requireStatus(t, performRequest(t, app, http.MethodDelete, "/platform", nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, app, http.MethodDelete, "/platform/deleted", nil), http.StatusNoContent)
	requireStatus(t, performRequest(t, app, http.MethodGet, "/platform/deleted", nil), http.StatusNotFound)
}

func performRequest(t *testing.T, app *fiberv3.App, method string, target string, body any) *http.Response {
	t.Helper()
	return performRequestWithHeaders(t, app, method, target, body, nil)
}

func performRequestWithHeaders(t *testing.T, app *fiberv3.App, method string, target string, body any, headers map[string]string) *http.Response {
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

	request, err := http.NewRequest(method, target, requestBody)
	if err != nil {
		t.Fatalf("http.NewRequest returned error: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Add(name, value)
	}
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("app.Test returned error: %v", err)
	}
	return response
}

func requireStatus(t *testing.T, response *http.Response, status int) {
	t.Helper()
	if response.StatusCode != status {
		t.Fatalf("expected status %d, got %d with body %s", status, response.StatusCode, readBody(t, response))
	}
}

func decodeJSON(t *testing.T, response *http.Response, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("json decode returned error: %v", err)
	}
}

func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	return string(body)
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
