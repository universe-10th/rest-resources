package echo

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	echov4 "github.com/labstack/echo/v4"
	"github.com/universe-10th/rest-resources/memory"
	"github.com/universe-10th/rest-resources/types/services"
	"github.com/universe-10th/rest-resources/utils"
)

type testService struct {
	prefix      string
	urlArg      string
	singleton   bool
	softDeleted bool
	verbs       utils.Flags[services.ResourceVerb]
	parent      services.Service
	children    []services.Service
	collections []services.ExtraEndpoint
	elements    []services.ExtraEndpoint
	middlewares []services.MiddlewareFunc
}

type panicParentService struct {
	*testService
}

func (service panicParentService) Parent() services.Service {
	panic("boom")
}

type fakeGroupProvider struct{}

func (fakeGroupProvider) Group(string, ...echov4.MiddlewareFunc) *echov4.Group {
	panic("unexpected Group")
}

func (service *testService) Prefix() string                            { return service.prefix }
func (service *testService) URLArg() string                            { return service.urlArg }
func (service *testService) IsSingleton() bool                         { return service.singleton }
func (service *testService) Verbs() utils.Flags[services.ResourceVerb] { return service.verbs }
func (service *testService) CanHaveChildren() bool                     { return service.Verbs().Has(services.ResourceGet) }
func (service *testService) IsSoftDeleted() bool                       { return service.softDeleted }
func (service *testService) Children() []services.Service              { return service.children }
func (service *testService) CollectionExtras() []services.ExtraEndpoint {
	return service.collections
}
func (service *testService) ElementExtras() []services.ExtraEndpoint { return service.elements }
func (service *testService) Parent() services.Service                { return service.parent }
func (service *testService) Middlewares() []services.MiddlewareFunc {
	return service.middlewares
}
func (service *testService) ElementMiddleware(bool) services.MiddlewareFunc {
	return func(next services.HandlerFunc) services.HandlerFunc {
		return func(context services.Context) error {
			context.SetData("element", true)
			context.PushElement("element")
			defer context.PopElement()
			return next(context)
		}
	}
}
func (service *testService) List(context services.Context, deleted bool) error {
	endpointType, verb, name := context.CurrentEndpoint()
	middlewareValue, _ := context.GetData("middleware")
	return context.RenderJSON(http.StatusOK, map[string]any{
		"deleted":     deleted,
		"endpoint":    endpointType,
		"verb":        verb,
		"name":        name,
		"middleware":  middlewareValue,
		"native_echo": context.Native() != nil,
	})
}
func (service *testService) Create(context services.Context) error {
	return context.RenderJSON(http.StatusCreated, map[string]any{"created": true})
}
func (service *testService) Get(context services.Context) error {
	_, hasElement := context.PeekElement(0)
	return context.RenderJSON(http.StatusOK, map[string]any{"element": hasElement})
}
func (service *testService) Update(context services.Context) error {
	return context.RenderNoContent(http.StatusNoContent)
}
func (service *testService) Delete(context services.Context) error {
	return context.RenderNoContent(http.StatusNoContent)
}
func (service *testService) Prune(context services.Context) error {
	return context.RenderNoContent(http.StatusNoContent)
}
func (service *testService) Restore(context services.Context) error {
	return context.RenderJSON(http.StatusOK, map[string]any{"restored": true})
}

func TestInstallRejectsInvalidRootInputs(t *testing.T) {
	t.Parallel()

	if err := Install(nil, &testService{}); !errors.Is(err, ErrInvalidEchoApp) {
		t.Fatalf("expected ErrInvalidEchoApp, got %v", err)
	}

	app := echov4.New()
	if err := Install(app, nil); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("expected ErrInvalidService, got %v", err)
	}

	parent := &testService{prefix: "parents"}
	child := &testService{prefix: "children", parent: parent}
	if err := Install(app, child); !errors.Is(err, ErrInvalidRootService) {
		t.Fatalf("expected ErrInvalidRootService, got %v", err)
	}
}

func TestMustInstallPanicsForInvalidRootInputs(t *testing.T) {
	t.Parallel()

	requirePanic(t, ErrInvalidEchoApp, func() {
		MustInstall(nil, &testService{})
	})
	requirePanic(t, ErrInvalidService, func() {
		MustInstall(echov4.New(), nil)
	})
	parent := &testService{prefix: "parents"}
	child := &testService{prefix: "children", parent: parent}
	requirePanic(t, ErrInvalidRootService, func() {
		MustInstall(echov4.New(), child)
	})
}

func TestInstallRethrowsNonErrorPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		value := recover()
		if value != "boom" {
			t.Fatalf("expected boom panic, got %#v", value)
		}
	}()
	_ = Install(echov4.New(), panicParentService{testService: &testService{prefix: "items"}})
}

func TestIsNilGroupProviderWithValueProvider(t *testing.T) {
	t.Parallel()

	if isNilGroupProvider(fakeGroupProvider{}) {
		t.Fatal("expected value group provider not to be nil")
	}
}

func TestContextRequestAndResponseHelpers(t *testing.T) {
	t.Parallel()

	app := echov4.New()
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
				_, missingPathErr := context.GetPathParam("missing")
				_, missingQueryErr := context.GetQueryParam("missing")
				_, missingQueriesErr := context.GetQueryParams("missing")
				_, missingHeaderErr := context.GetHeader("missing")
				_, missingHeadersErr := context.GetHeaders("missing")
				_, missingCookieErr := context.GetCookie("missing")
				_, popOK := context.PopElement()

				context.SetHeader("X-Result", "ok")
				context.SetCookie(services.Cookie{
					Name:     "seen",
					Value:    "yes",
					Path:     "/",
					SameSite: services.CookieSameSiteStrict,
				})
				return context.RenderJSON(http.StatusOK, map[string]any{
					"name":            body.Name,
					"first_tag":       firstTag,
					"tag_count":       len(allTags),
					"header":          header,
					"header_count":    len(headers),
					"cookie":          cookie.Value,
					"cookie_samesite": cookie.SameSite,
					"missing_path":    missingPathErr == nil,
					"missing_query":   missingQueryErr == nil,
					"missing_queries": missingQueriesErr == nil,
					"missing_header":  missingHeaderErr == nil,
					"missing_headers": missingHeadersErr == nil,
					"missing_cookie":  missingCookieErr == nil,
					"pop":             popOK,
					"native":          context.Native() != nil,
				})
			},
		}},
	}
	if err := Install(app, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	response := performJSONRequestWithHeaders(t, app, http.MethodPost, "/items/inspect?tag=a&tag=b", map[string]any{"name": "Main"}, map[string]string{
		"X-Test": "one",
		"Cookie": "session=abc",
	})
	requireStatus(t, response, http.StatusOK)
	if response.Header().Get("X-Result") != "ok" {
		t.Fatalf("expected X-Result header, got %q", response.Header().Get("X-Result"))
	}
	if cookie := response.Header().Get("Set-Cookie"); !containsAll(cookie, "seen=yes", "SameSite=Strict") {
		t.Fatalf("expected Set-Cookie header, got %q", cookie)
	}
	if body := response.Body.String(); !containsAll(
		body,
		`"name":"Main"`,
		`"first_tag":"a"`,
		`"tag_count":2`,
		`"header":"one"`,
		`"header_count":1`,
		`"cookie":"abc"`,
		`"cookie_samesite":0`,
		`"missing_path":false`,
		`"missing_query":false`,
		`"missing_queries":false`,
		`"missing_header":false`,
		`"missing_headers":false`,
		`"missing_cookie":false`,
		`"pop":false`,
		`"native":true`,
	) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestContextResponseAlreadySent(t *testing.T) {
	t.Parallel()

	app := echov4.New()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	native := app.NewContext(request, response)
	context := WrapContext(native)

	if context.ResponseAlreadySent() {
		t.Fatal("expected response not to be sent before rendering")
	}
	if err := context.RenderNoContent(http.StatusNoContent); err != nil {
		t.Fatalf("RenderNoContent returned error: %v", err)
	}
	if !context.ResponseAlreadySent() {
		t.Fatal("expected response to be sent after rendering")
	}
}

func TestSameSiteMapping(t *testing.T) {
	t.Parallel()

	if fromHTTPSameSite(http.SameSiteLaxMode) != services.CookieSameSiteLax {
		t.Fatal("expected Lax same-site mapping from HTTP")
	}
	if fromHTTPSameSite(http.SameSiteStrictMode) != services.CookieSameSiteStrict {
		t.Fatal("expected Strict same-site mapping from HTTP")
	}
	if fromHTTPSameSite(http.SameSiteNoneMode) != services.CookieSameSiteNone {
		t.Fatal("expected None same-site mapping from HTTP")
	}
	if fromHTTPSameSite(http.SameSiteDefaultMode) != services.CookieSameSiteDefault {
		t.Fatal("expected Default same-site mapping from HTTP")
	}

	if toHTTPSameSite(services.CookieSameSiteLax) != http.SameSiteLaxMode {
		t.Fatal("expected Lax same-site mapping to HTTP")
	}
	if toHTTPSameSite(services.CookieSameSiteStrict) != http.SameSiteStrictMode {
		t.Fatal("expected Strict same-site mapping to HTTP")
	}
	if toHTTPSameSite(services.CookieSameSiteNone) != http.SameSiteNoneMode {
		t.Fatal("expected None same-site mapping to HTTP")
	}
	if toHTTPSameSite(services.CookieSameSiteDefault) != http.SameSiteDefaultMode {
		t.Fatal("expected Default same-site mapping to HTTP")
	}
}

func TestInstallWrapsMiddlewareAndHandlerContext(t *testing.T) {
	t.Parallel()

	app := echov4.New()
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

	request := httptest.NewRequest(http.MethodGet, "/items", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); body == "" || !containsAll(body, `"middleware":"seen"`, `"verb":1`, `"native_echo":true`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestInstallWrapsElementMiddleware(t *testing.T) {
	t.Parallel()

	app := echov4.New()
	service := &testService{
		prefix: "items",
		urlArg: "item_id",
		verbs:  utils.NewFlags(services.ResourceGet),
	}

	if err := Install(app, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/items/42", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); body == "" || !containsAll(body, `"element":true`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestInstallCollectionExtra(t *testing.T) {
	t.Parallel()

	app := echov4.New()
	service := &testService{
		prefix: "items",
		urlArg: "item_id",
		verbs:  utils.NewFlags(services.ResourceGet, services.ResourceList),
		collections: []services.ExtraEndpoint{
			{
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
			},
		},
	}

	if err := Install(app, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/items/search", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d with body %s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); body == "" || !containsAll(body, `"endpoint":1`, `"verb":0`, `"name":"search"`, `"element":false`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestInstallElementExtra(t *testing.T) {
	t.Parallel()

	app := echov4.New()
	service := &testService{
		prefix: "items",
		urlArg: "item_id",
		verbs:  utils.NewFlags(services.ResourceList),
		elements: []services.ExtraEndpoint{
			{
				Method: http.MethodPut,
				Name:   "publish",
				Handler: func(context services.Context) error {
					endpointType, verb, name := context.CurrentEndpoint()
					_, hasElement := context.PeekElement(0)
					middlewareValue, _ := context.GetData("middleware")
					return context.RenderJSON(http.StatusOK, map[string]any{
						"endpoint":   endpointType,
						"verb":       verb,
						"name":       name,
						"element":    hasElement,
						"middleware": middlewareValue,
					})
				},
			},
		},
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

	request := httptest.NewRequest(http.MethodPut, "/items/42/publish", nil)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); body == "" || !containsAll(body, `"endpoint":2`, `"verb":0`, `"name":"publish"`, `"element":true`, `"middleware":"seen"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestInstallSingletonElementExtra(t *testing.T) {
	t.Parallel()

	app := echov4.New()
	storage := memory.NewStorage[int, *integrationSetting]()
	service := services.MustCreateSingletonService[int, *integrationSetting]("platform", storage)
	service.MustAddElementExtra("GET", "statistics", func(context services.Context) error {
		endpointType, _, name := context.CurrentEndpoint()
		elementRaw, ok := context.PeekElement(0)
		if !ok {
			return context.RenderJSON(http.StatusInternalServerError, map[string]any{"error": "missing element"})
		}
		element := elementRaw.(*integrationSetting)
		return context.RenderJSON(http.StatusOK, map[string]any{
			"endpoint": endpointType,
			"name":     name,
			"version":  element.Version,
		})
	})

	if err := Install(app, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	createResponse := performJSONRequest(t, app, http.MethodPost, "/platform", map[string]any{"version": "2026.9"})
	requireStatus(t, createResponse, http.StatusCreated)

	extraResponse := performJSONRequest(t, app, http.MethodGet, "/platform/statistics", nil)
	requireStatus(t, extraResponse, http.StatusOK)
	if body := extraResponse.Body.String(); !containsAll(body, `"endpoint":2`, `"name":"statistics"`, `"version":"2026.9"`) {
		t.Fatalf("unexpected singleton extra response body: %s", body)
	}
}

func TestInstallSoftDeletedSingletonRoutes(t *testing.T) {
	t.Parallel()

	app := echov4.New()
	service := services.MustCreateSingletonService[int, *integrationSoftSetting]("platform", memory.NewStorage[int, *integrationSoftSetting]())
	if err := Install(app, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	requireStatus(t, performJSONRequest(t, app, http.MethodPost, "/platform", map[string]any{"version": "2026.9"}), http.StatusCreated)
	requireStatus(t, performJSONRequest(t, app, http.MethodDelete, "/platform", nil), http.StatusNoContent)
	requireStatus(t, performJSONRequest(t, app, http.MethodGet, "/platform/deleted", nil), http.StatusOK)
	requireStatus(t, performJSONRequest(t, app, http.MethodPost, "/platform/deleted", nil), http.StatusOK)
	requireStatus(t, performJSONRequest(t, app, http.MethodDelete, "/platform", nil), http.StatusNoContent)
	requireStatus(t, performJSONRequest(t, app, http.MethodDelete, "/platform/deleted", nil), http.StatusNoContent)
	requireStatus(t, performJSONRequest(t, app, http.MethodGet, "/platform/deleted", nil), http.StatusNotFound)
}

func TestInstallElementExtraUsesLiveResourceElement(t *testing.T) {
	t.Parallel()

	app := echov4.New()
	storage := memory.NewStorage[int, *integrationStore]()
	service := services.MustCreateCollectionService[int, *integrationStore]("stores", "store_id", storage)
	service.MustAddElementExtra("GET", "summary", func(context services.Context) error {
		elementRaw, ok := context.PeekElement(0)
		if !ok {
			return context.RenderJSON(http.StatusInternalServerError, map[string]any{"error": "missing element"})
		}
		element := elementRaw.(*integrationStore)
		return service.RenderElement(context, http.StatusOK, element)
	})
	service.UsingElementRenderer(func(context services.Context, store *integrationStore) error {
		endpointType, _, name := context.CurrentEndpoint()
		return context.RenderJSON(http.StatusOK, map[string]any{
			"endpoint": endpointType,
			"name":     name,
			"id":       store.ID,
			"store":    store.Name,
		})
	})

	if err := Install(app, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	createResponse := performJSONRequest(t, app, http.MethodPost, "/stores", map[string]any{"name": "Main"})
	requireStatus(t, createResponse, http.StatusOK)
	var createdStore integrationStore
	decodeJSON(t, createResponse, &createdStore)

	extraResponse := performJSONRequest(t, app, http.MethodGet, "/stores/"+strconv.Itoa(createdStore.ID)+"/summary", nil)
	requireStatus(t, extraResponse, http.StatusOK)
	if body := extraResponse.Body.String(); !containsAll(body, `"endpoint":2`, `"name":"summary"`, `"store":"Main"`) {
		t.Fatalf("unexpected element extra response body: %s", body)
	}

	deleteResponse := performJSONRequest(t, app, http.MethodDelete, "/stores/"+strconv.Itoa(createdStore.ID), nil)
	requireStatus(t, deleteResponse, http.StatusNoContent)

	deletedExtraResponse := performJSONRequest(t, app, http.MethodGet, "/stores/"+strconv.Itoa(createdStore.ID)+"/summary", nil)
	requireStatus(t, deletedExtraResponse, http.StatusNotFound)
}

func TestInstallAcceptsEchoGroup(t *testing.T) {
	t.Parallel()

	app := echov4.New()
	group := app.Group("/api")
	service := &testService{
		prefix: "items",
		urlArg: "item_id",
		verbs:  utils.NewFlags(services.ResourceList),
	}

	if err := Install(group, service); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	rootRequest := httptest.NewRequest(http.MethodGet, "/items", nil)
	rootResponse := httptest.NewRecorder()
	app.ServeHTTP(rootResponse, rootRequest)
	if rootResponse.Code != http.StatusNotFound {
		t.Fatalf("expected root route status 404, got %d", rootResponse.Code)
	}

	groupRequest := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	groupResponse := httptest.NewRecorder()
	app.ServeHTTP(groupResponse, groupRequest)
	if groupResponse.Code != http.StatusOK {
		t.Fatalf("expected grouped route status 200, got %d with body %s", groupResponse.Code, groupResponse.Body.String())
	}
}

type integrationStore struct {
	memory.SoftDeletedResource[int]
	Name string `json:"name"`
}

type integrationCatalog struct {
	memory.Resource[int]
	StoreID int    `json:"store_id"`
	Name    string `json:"name"`
}

type integrationProduct struct {
	memory.SoftDeletedResource[int]
	CatalogID int    `json:"catalog_id"`
	Name      string `json:"name"`
	Rank      int    `json:"rank"`
}

type integrationSetting struct {
	memory.Resource[int]
	Version string `json:"version"`
}

type integrationSoftSetting struct {
	memory.SoftDeletedResource[int]
	Version string `json:"version"`
}

type integrationHardItem struct {
	memory.Resource[int]
	Name string `json:"name"`
}

type integrationPlatformProfile struct {
	memory.Resource[int]
	Name string `json:"name"`
}

type integrationStoreProfile struct {
	memory.Resource[int]
	StoreID int    `json:"store_id"`
	Note    string `json:"note"`
}

type integrationPlatformEvent struct {
	memory.Resource[int]
	Name string `json:"name"`
}

/*
Route documentation for this platform integration.

The test installs three root services:
  - /stores: soft-deleted collection. Its live element group is /stores/:store_id.
  - /platform: singleton.
  - /hard-items: hard-deleted collection. Its live element group is /hard-items/:hard_item_id.

Then it attaches:
  - /stores/:store_id/catalogs: collection constrained by catalog.store_id == store.id.
  - /stores/:store_id/catalogs/:catalog_id/products: soft-deleted collection constrained by product.catalog_id == catalog.id.

Middleware names below are the service-neutral middleware functions wrapped for Echo:
  - setup(S, V): setupMiddleware(service=S, endpointType=EndpointVerb, verb=V, name="").
  - element(S, deleted): services.ElementMiddleware for service S, loading the current element and pushing it on the context stack.
  - service middlewares: service.Middlewares(); this test installs one custom middleware per service.

Created URLs and middleware chains:

  - POST /platform
    setup(platform, ResourceCreate)
    Accessed by this test to create the singleton.

  - GET /platform
    setup(platform, ResourceGet) -> element(platform, false)
    Accessed by this test to read the singleton.

  - PATCH /platform
    setup(platform, ResourceUpdate) -> element(platform, false)

  - DELETE /platform
    setup(platform, ResourceDelete) -> element(platform, false)

  - GET /stores
    setup(stores, ResourceList)

  - POST /stores
    setup(stores, ResourceCreate)
    Accessed by this test to create the parent store.

  - GET /stores/:store_id
    setup(stores, ResourceGet) -> element(stores, false)

  - PATCH /stores/:store_id
    setup(stores, ResourceUpdate) -> element(stores, false)

  - DELETE /stores/:store_id
    setup(stores, ResourceDelete) -> element(stores, false)

  - GET /stores/deleted
    setup(stores, ResourceListDeleted)

  - GET /stores/deleted/:store_id
    setup(stores, ResourceGetDeleted) -> element(stores, true)

  - POST /stores/deleted/:store_id
    setup(stores, ResourceRestore) -> element(stores, true)

  - DELETE /stores/deleted/:store_id
    setup(stores, ResourcePrune) -> element(stores, true)

  - GET /stores/:store_id/catalogs
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceList)

  - POST /stores/:store_id/catalogs
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceCreate)

  - GET /stores/:store_id/catalogs/:catalog_id
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceGet) -> element(catalogs, false)

  - PATCH /stores/:store_id/catalogs/:catalog_id
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceUpdate) -> element(catalogs, false)

  - DELETE /stores/:store_id/catalogs/:catalog_id
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceDelete) -> element(catalogs, false)

  - GET /stores/:store_id/catalogs/:catalog_id/products
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceGet) -> element(catalogs, false) ->
    setup(products, ResourceList)
    Accessed by this test with ?sort=rank to list live products.

  - POST /stores/:store_id/catalogs/:catalog_id/products
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceGet) -> element(catalogs, false) ->
    setup(products, ResourceCreate)

  - GET /stores/:store_id/catalogs/:catalog_id/products/:product_id
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceGet) -> element(catalogs, false) ->
    setup(products, ResourceGet) -> element(products, false)

  - PATCH /stores/:store_id/catalogs/:catalog_id/products/:product_id
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceGet) -> element(catalogs, false) ->
    setup(products, ResourceUpdate) -> element(products, false)

  - DELETE /stores/:store_id/catalogs/:catalog_id/products/:product_id
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceGet) -> element(catalogs, false) ->
    setup(products, ResourceDelete) -> element(products, false)
    Accessed by this test to soft-delete a product.

  - GET /stores/:store_id/catalogs/:catalog_id/products/deleted
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceGet) -> element(catalogs, false) ->
    setup(products, ResourceListDeleted)
    Accessed by this test to list deleted products.

  - GET /stores/:store_id/catalogs/:catalog_id/products/deleted/:product_id
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceGet) -> element(catalogs, false) ->
    setup(products, ResourceGetDeleted) -> element(products, true)

  - POST /stores/:store_id/catalogs/:catalog_id/products/deleted/:product_id
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceGet) -> element(catalogs, false) ->
    setup(products, ResourceRestore) -> element(products, true)
    Accessed by this test to restore a deleted product.

  - DELETE /stores/:store_id/catalogs/:catalog_id/products/deleted/:product_id
    setup(stores, ResourceGet) -> element(stores, false) ->
    setup(catalogs, ResourceGet) -> element(catalogs, false) ->
    setup(products, ResourcePrune) -> element(products, true)
    Accessed by this test first to prove restored products are no longer in the
    deleted route, then again to prune a deleted product.

  - POST /hard-items
    setup(hard-items, ResourceCreate)
    Accessed by this test to create a hard-deleted resource.

  - GET /hard-items/:hard_item_id
    setup(hard-items, ResourceGet) -> element(hard-items, false)
    Accessed by this test after hard deletion to assert 404.

  - DELETE /hard-items/:hard_item_id
    setup(hard-items, ResourceDelete) -> element(hard-items, false)
    Accessed by this test to permanently delete the hard-deleted resource.

The hard-items collection also creates GET /hard-items, PATCH /hard-items/:hard_item_id,
and no /deleted routes because integrationHardItem does not implement SoftDeletedResource.
Catalogs also create no /deleted routes because integrationCatalog is not soft-deleted.
*/
func TestEchoPlatformWithMemoryStorageNestedSingletonsCollectionsAndDeletes(t *testing.T) {
	t.Parallel()

	app := echov4.New()

	storeStorage := memory.NewStorage[int, *integrationStore]()
	catalogStorage := memory.NewStorage[int, *integrationCatalog]()
	productStorage := memory.NewStorage[int, *integrationProduct]()
	settingStorage := memory.NewStorage[int, *integrationSetting]()
	hardItemStorage := memory.NewStorage[int, *integrationHardItem]()
	platformProfileStorage := memory.NewStorage[int, *integrationPlatformProfile]()
	storeProfileStorage := memory.NewStorage[int, *integrationStoreProfile]()
	platformEventStorage := memory.NewStorage[int, *integrationPlatformEvent]()

	storeService := services.MustCreateCollectionService[int, *integrationStore]("stores", "store_id", storeStorage)
	storeService.UsingPageSize(5)
	storeService.UsingMiddlewares(integrationMiddleware("stores"))
	storeService.UsingElementRenderer(func(context services.Context, store *integrationStore) error {
		return context.RenderJSON(http.StatusOK, map[string]any{
			"id":       store.ID,
			"name":     store.Name,
			"rendered": "store-element",
		})
	})
	catalogService := services.MustCreateCollectionService[int, *integrationCatalog]("catalogs", "catalog_id", catalogStorage)
	catalogService.UsingPageSize(5)
	catalogService.UsingMiddlewares(integrationMiddleware("catalogs"))
	catalogService.MustAttachTo(storeService, "store_id")
	productService := services.MustCreateCollectionService[int, *integrationProduct]("products", "product_id", productStorage)
	productService.UsingPageSize(5)
	productService.UsingMiddlewares(integrationMiddleware("products"))
	productService.UsingPageRenderer(func(context services.Context, products []*integrationProduct, page int64, totalPages int64) error {
		return context.RenderJSON(http.StatusOK, map[string]any{
			"items":    products,
			"page":     page,
			"pages":    totalPages,
			"rendered": "products-page",
		})
	})
	productService.MustAttachTo(catalogService, "catalog_id")
	settingService := services.MustCreateSingletonService[int, *integrationSetting]("platform", settingStorage)
	settingService.UsingMiddlewares(integrationMiddleware("platform"))
	platformProfileService := services.MustCreateSingletonService[int, *integrationPlatformProfile]("profile", platformProfileStorage)
	platformProfileService.UsingMiddlewares(integrationMiddleware("platform-profile"))
	platformProfileService.MustAttachTo(settingService, "")
	platformEventService := services.MustCreateCollectionService[int, *integrationPlatformEvent]("events", "event_id", platformEventStorage)
	platformEventService.UsingMiddlewares(integrationMiddleware("platform-events"))
	platformEventService.MustAttachTo(settingService, "")
	storeProfileService := services.MustCreateSingletonService[int, *integrationStoreProfile]("profile", storeProfileStorage)
	storeProfileService.UsingMiddlewares(integrationMiddleware("store-profile"))
	storeProfileService.MustAttachTo(storeService, "store_id")
	hardItemService := services.MustCreateCollectionService[int, *integrationHardItem]("hard-items", "hard_item_id", hardItemStorage)
	hardItemService.UsingMiddlewares(integrationMiddleware("hard-items"))

	if err := Install(app, storeService); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if err := Install(app, settingService); err != nil {
		t.Fatalf("Install singleton returned error: %v", err)
	}
	if err := Install(app, hardItemService); err != nil {
		t.Fatalf("Install hard item service returned error: %v", err)
	}

	settingResponse := performJSONRequest(t, app, http.MethodPost, "/platform", map[string]any{"version": "2026.9"})
	requireStatus(t, settingResponse, http.StatusCreated)
	requireMiddleware(t, settingResponse, "platform")
	var createdSetting integrationSetting
	decodeJSON(t, settingResponse, &createdSetting)
	if createdSetting.Version != "2026.9" || createdSetting.ID == 0 {
		t.Fatalf("unexpected singleton create response: %#v", createdSetting)
	}

	storeResponse := performJSONRequest(t, app, http.MethodPost, "/stores", map[string]any{"name": "Main"})
	requireStatus(t, storeResponse, http.StatusOK)
	requireMiddleware(t, storeResponse, "stores")
	var createdStore struct {
		ID       int    `json:"id"`
		Name     string `json:"name"`
		Rendered string `json:"rendered"`
	}
	decodeJSON(t, storeResponse, &createdStore)
	if createdStore.ID == 0 || createdStore.Rendered != "store-element" {
		t.Fatalf("unexpected rendered store response: %#v", createdStore)
	}

	hardItemResponse := performJSONRequest(t, app, http.MethodPost, "/hard-items", map[string]any{"name": "Temporary"})
	requireStatus(t, hardItemResponse, http.StatusCreated)
	requireMiddleware(t, hardItemResponse, "hard-items")
	var hardItem integrationHardItem
	decodeJSON(t, hardItemResponse, &hardItem)

	storesListResponse := performJSONRequest(t, app, http.MethodGet, "/stores", nil)
	requireStatus(t, storesListResponse, http.StatusOK)
	requireMiddleware(t, storesListResponse, "stores")

	storeGetResponse := performJSONRequest(t, app, http.MethodGet, "/stores/1", nil)
	requireStatus(t, storeGetResponse, http.StatusOK)
	requireMiddleware(t, storeGetResponse, "stores")
	if !contains(storeGetResponse.Body.String(), `"rendered":"store-element"`) {
		t.Fatalf("expected custom store renderer response, got %s", storeGetResponse.Body.String())
	}

	storePatchResponse := performJSONRequest(t, app, http.MethodPatch, "/stores/1", map[string]any{"name": "Main Updated"})
	requireStatus(t, storePatchResponse, http.StatusOK)
	requireMiddleware(t, storePatchResponse, "stores")
	if !contains(storePatchResponse.Body.String(), `"name":"Main Updated"`) {
		t.Fatalf("expected patched store name, got %s", storePatchResponse.Body.String())
	}

	catalogResponse := performJSONRequest(t, app, http.MethodPost, "/stores/1/catalogs", map[string]any{"name": "Fall"})
	requireStatus(t, catalogResponse, http.StatusCreated)
	requireMiddleware(t, catalogResponse, "stores", "catalogs")
	var catalog integrationCatalog
	decodeJSON(t, catalogResponse, &catalog)
	if catalog.ID == 0 || catalog.StoreID != createdStore.ID {
		t.Fatalf("unexpected catalog response: %#v", catalog)
	}

	catalogsListResponse := performJSONRequest(t, app, http.MethodGet, "/stores/1/catalogs", nil)
	requireStatus(t, catalogsListResponse, http.StatusOK)
	requireMiddleware(t, catalogsListResponse, "stores", "catalogs")

	catalogGetResponse := performJSONRequest(t, app, http.MethodGet, "/stores/1/catalogs/1", nil)
	requireStatus(t, catalogGetResponse, http.StatusOK)
	requireMiddleware(t, catalogGetResponse, "stores", "catalogs")

	catalogPatchResponse := performJSONRequest(t, app, http.MethodPatch, "/stores/1/catalogs/1", map[string]any{"name": "Fall Updated"})
	requireStatus(t, catalogPatchResponse, http.StatusOK)
	requireMiddleware(t, catalogPatchResponse, "stores", "catalogs")
	var patchedCatalog integrationCatalog
	decodeJSON(t, catalogPatchResponse, &patchedCatalog)
	if patchedCatalog.StoreID != createdStore.ID || patchedCatalog.Name != "Fall Updated" {
		t.Fatalf("unexpected patched catalog response: %#v", patchedCatalog)
	}

	firstProductResponse := performJSONRequest(t, app, http.MethodPost, "/stores/1/catalogs/1/products", map[string]any{"name": "Hat", "rank": 2})
	requireStatus(t, firstProductResponse, http.StatusCreated)
	requireMiddleware(t, firstProductResponse, "stores", "catalogs", "products")
	var firstProduct integrationProduct
	decodeJSON(t, firstProductResponse, &firstProduct)
	if firstProduct.ID == 0 || firstProduct.CatalogID != catalog.ID {
		t.Fatalf("unexpected first product response: %#v", firstProduct)
	}

	secondProductResponse := performJSONRequest(t, app, http.MethodPost, "/stores/1/catalogs/1/products", map[string]any{"name": "Scarf", "rank": 1})
	requireStatus(t, secondProductResponse, http.StatusCreated)
	requireMiddleware(t, secondProductResponse, "stores", "catalogs", "products")
	var secondProduct integrationProduct
	decodeJSON(t, secondProductResponse, &secondProduct)
	if secondProduct.ID == 0 || secondProduct.CatalogID != catalog.ID {
		t.Fatalf("unexpected second product response: %#v", secondProduct)
	}

	settingResponse = performJSONRequest(t, app, http.MethodGet, "/platform", nil)
	requireStatus(t, settingResponse, http.StatusOK)
	requireMiddleware(t, settingResponse, "platform")
	var loadedSetting integrationSetting
	decodeJSON(t, settingResponse, &loadedSetting)
	if loadedSetting.Version != "2026.9" {
		t.Fatalf("unexpected singleton setting response: %#v", loadedSetting)
	}

	settingPatchResponse := performJSONRequest(t, app, http.MethodPatch, "/platform", map[string]any{"version": "2026.10"})
	requireStatus(t, settingPatchResponse, http.StatusOK)
	requireMiddleware(t, settingPatchResponse, "platform")
	var patchedSetting integrationSetting
	decodeJSON(t, settingPatchResponse, &patchedSetting)
	if patchedSetting.Version != "2026.10" {
		t.Fatalf("unexpected patched singleton response: %#v", patchedSetting)
	}

	platformProfileResponse := performJSONRequest(t, app, http.MethodPost, "/platform/profile", map[string]any{"name": "Root Profile"})
	requireStatus(t, platformProfileResponse, http.StatusCreated)
	requireMiddleware(t, platformProfileResponse, "platform", "platform-profile")
	var platformProfile integrationPlatformProfile
	decodeJSON(t, platformProfileResponse, &platformProfile)
	if platformProfile.ID == 0 || platformProfile.Name != "Root Profile" {
		t.Fatalf("unexpected singleton child of singleton response: %#v", platformProfile)
	}

	platformProfileGetResponse := performJSONRequest(t, app, http.MethodGet, "/platform/profile", nil)
	requireStatus(t, platformProfileGetResponse, http.StatusOK)
	requireMiddleware(t, platformProfileGetResponse, "platform", "platform-profile")

	platformEventResponse := performJSONRequest(t, app, http.MethodPost, "/platform/events", map[string]any{"name": "Launch"})
	requireStatus(t, platformEventResponse, http.StatusCreated)
	requireMiddleware(t, platformEventResponse, "platform", "platform-events")
	var platformEvent integrationPlatformEvent
	decodeJSON(t, platformEventResponse, &platformEvent)
	if platformEvent.ID == 0 || platformEvent.Name != "Launch" {
		t.Fatalf("unexpected collection child of singleton response: %#v", platformEvent)
	}

	platformEventsListResponse := performJSONRequest(t, app, http.MethodGet, "/platform/events", nil)
	requireStatus(t, platformEventsListResponse, http.StatusOK)
	requireMiddleware(t, platformEventsListResponse, "platform", "platform-events")

	platformEventGetResponse := performJSONRequest(t, app, http.MethodGet, "/platform/events/"+strconv.Itoa(platformEvent.ID), nil)
	requireStatus(t, platformEventGetResponse, http.StatusOK)
	requireMiddleware(t, platformEventGetResponse, "platform", "platform-events")

	storeProfileResponse := performJSONRequest(t, app, http.MethodPost, "/stores/1/profile", map[string]any{"note": "Store scoped"})
	requireStatus(t, storeProfileResponse, http.StatusCreated)
	requireMiddleware(t, storeProfileResponse, "stores", "store-profile")
	var storeProfile integrationStoreProfile
	decodeJSON(t, storeProfileResponse, &storeProfile)
	if storeProfile.ID == 0 || storeProfile.StoreID != createdStore.ID {
		t.Fatalf("unexpected singleton child of collection response: %#v", storeProfile)
	}

	storeProfileGetResponse := performJSONRequest(t, app, http.MethodGet, "/stores/1/profile", nil)
	requireStatus(t, storeProfileGetResponse, http.StatusOK)
	requireMiddleware(t, storeProfileGetResponse, "stores", "store-profile")

	productsResponse := performJSONRequest(t, app, http.MethodGet, "/stores/1/catalogs/1/products?sort=rank", nil)
	requireStatus(t, productsResponse, http.StatusOK)
	requireMiddleware(t, productsResponse, "stores", "catalogs", "products")
	var productsPage struct {
		Items    []integrationProduct `json:"items"`
		Page     int                  `json:"page"`
		Pages    int                  `json:"pages"`
		Rendered string               `json:"rendered"`
	}
	decodeJSON(t, productsResponse, &productsPage)
	if productsPage.Pages != 1 || productsPage.Rendered != "products-page" || len(productsPage.Items) != 2 || productsPage.Items[0].Name != "Scarf" {
		t.Fatalf("unexpected products page: %#v", productsPage)
	}

	productGetResponse := performJSONRequest(t, app, http.MethodGet, "/stores/1/catalogs/1/products/1", nil)
	requireStatus(t, productGetResponse, http.StatusOK)
	requireMiddleware(t, productGetResponse, "stores", "catalogs", "products")

	productPatchResponse := performJSONRequest(t, app, http.MethodPatch, "/stores/1/catalogs/1/products/1", map[string]any{"name": "Hat Updated", "rank": 3})
	requireStatus(t, productPatchResponse, http.StatusOK)
	requireMiddleware(t, productPatchResponse, "stores", "catalogs", "products")
	var patchedProduct integrationProduct
	decodeJSON(t, productPatchResponse, &patchedProduct)
	if patchedProduct.Name != "Hat Updated" || patchedProduct.CatalogID != catalog.ID {
		t.Fatalf("unexpected patched product response: %#v", patchedProduct)
	}

	deleteProductResponse := performJSONRequest(t, app, http.MethodDelete, "/stores/1/catalogs/1/products/1", nil)
	requireStatus(t, deleteProductResponse, http.StatusNoContent)
	requireMiddleware(t, deleteProductResponse, "stores", "catalogs", "products")

	deletedProductsResponse := performJSONRequest(t, app, http.MethodGet, "/stores/1/catalogs/1/products/deleted", nil)
	requireStatus(t, deletedProductsResponse, http.StatusOK)
	requireMiddleware(t, deletedProductsResponse, "stores", "catalogs", "products")
	var deletedProductsPage struct {
		Items    []integrationProduct `json:"items"`
		Rendered string               `json:"rendered"`
	}
	decodeJSON(t, deletedProductsResponse, &deletedProductsPage)
	if deletedProductsPage.Rendered != "products-page" || len(deletedProductsPage.Items) != 1 || deletedProductsPage.Items[0].Name != "Hat Updated" {
		t.Fatalf("unexpected deleted products page: %#v", deletedProductsPage)
	}

	deletedProductGetResponse := performJSONRequest(t, app, http.MethodGet, "/stores/1/catalogs/1/products/deleted/1", nil)
	requireStatus(t, deletedProductGetResponse, http.StatusOK)
	requireMiddleware(t, deletedProductGetResponse, "stores", "catalogs", "products")

	restoreProductResponse := performJSONRequest(t, app, http.MethodPost, "/stores/1/catalogs/1/products/deleted/1", nil)
	requireStatus(t, restoreProductResponse, http.StatusOK)
	requireMiddleware(t, restoreProductResponse, "stores", "catalogs", "products")
	pruneAfterRestoreResponse := performJSONRequest(t, app, http.MethodDelete, "/stores/1/catalogs/1/products/deleted/1", nil)
	requireStatus(t, pruneAfterRestoreResponse, http.StatusNotFound)
	requireMiddleware(t, pruneAfterRestoreResponse, "stores", "catalogs")

	deleteProductAgainResponse := performJSONRequest(t, app, http.MethodDelete, "/stores/1/catalogs/1/products/1", nil)
	requireStatus(t, deleteProductAgainResponse, http.StatusNoContent)
	requireMiddleware(t, deleteProductAgainResponse, "stores", "catalogs", "products")
	pruneProductResponse := performJSONRequest(t, app, http.MethodDelete, "/stores/1/catalogs/1/products/deleted/1", nil)
	requireStatus(t, pruneProductResponse, http.StatusNoContent)
	requireMiddleware(t, pruneProductResponse, "stores", "catalogs", "products")

	deleteCatalogResponse := performJSONRequest(t, app, http.MethodDelete, "/stores/1/catalogs/1", nil)
	requireStatus(t, deleteCatalogResponse, http.StatusNoContent)
	requireMiddleware(t, deleteCatalogResponse, "stores", "catalogs")

	missingCatalogResponse := performJSONRequest(t, app, http.MethodGet, "/stores/1/catalogs/1", nil)
	requireStatus(t, missingCatalogResponse, http.StatusNotFound)
	requireMiddleware(t, missingCatalogResponse, "stores")

	hardItemsListResponse := performJSONRequest(t, app, http.MethodGet, "/hard-items", nil)
	requireStatus(t, hardItemsListResponse, http.StatusOK)
	requireMiddleware(t, hardItemsListResponse, "hard-items")

	hardItemGetResponse := performJSONRequest(t, app, http.MethodGet, "/hard-items/"+strconv.Itoa(hardItem.ID), nil)
	requireStatus(t, hardItemGetResponse, http.StatusOK)
	requireMiddleware(t, hardItemGetResponse, "hard-items")

	hardItemPatchResponse := performJSONRequest(t, app, http.MethodPatch, "/hard-items/"+strconv.Itoa(hardItem.ID), map[string]any{"name": "Temporary Updated"})
	requireStatus(t, hardItemPatchResponse, http.StatusOK)
	requireMiddleware(t, hardItemPatchResponse, "hard-items")

	deleteHardItemResponse := performJSONRequest(t, app, http.MethodDelete, "/hard-items/"+strconv.Itoa(hardItem.ID), nil)
	requireStatus(t, deleteHardItemResponse, http.StatusNoContent)
	requireMiddleware(t, deleteHardItemResponse, "hard-items")
	missingHardItemResponse := performJSONRequest(t, app, http.MethodGet, "/hard-items/"+strconv.Itoa(hardItem.ID), nil)
	requireStatus(t, missingHardItemResponse, http.StatusNotFound)
	requireMiddleware(t, missingHardItemResponse, "hard-items")

	storeDeleteResponse := performJSONRequest(t, app, http.MethodDelete, "/stores/1", nil)
	requireStatus(t, storeDeleteResponse, http.StatusNoContent)
	requireMiddleware(t, storeDeleteResponse, "stores")

	deletedStoresResponse := performJSONRequest(t, app, http.MethodGet, "/stores/deleted", nil)
	requireStatus(t, deletedStoresResponse, http.StatusOK)
	requireMiddleware(t, deletedStoresResponse, "stores")

	deletedStoreGetResponse := performJSONRequest(t, app, http.MethodGet, "/stores/deleted/1", nil)
	requireStatus(t, deletedStoreGetResponse, http.StatusOK)
	requireMiddleware(t, deletedStoreGetResponse, "stores")

	storeRestoreResponse := performJSONRequest(t, app, http.MethodPost, "/stores/deleted/1", nil)
	requireStatus(t, storeRestoreResponse, http.StatusOK)
	requireMiddleware(t, storeRestoreResponse, "stores")

	storeDeleteAgainResponse := performJSONRequest(t, app, http.MethodDelete, "/stores/1", nil)
	requireStatus(t, storeDeleteAgainResponse, http.StatusNoContent)
	requireMiddleware(t, storeDeleteAgainResponse, "stores")

	storePruneResponse := performJSONRequest(t, app, http.MethodDelete, "/stores/deleted/1", nil)
	requireStatus(t, storePruneResponse, http.StatusNoContent)
	requireMiddleware(t, storePruneResponse, "stores")

	platformEventDeleteResponse := performJSONRequest(t, app, http.MethodDelete, "/platform/events/"+strconv.Itoa(platformEvent.ID), nil)
	requireStatus(t, platformEventDeleteResponse, http.StatusNoContent)
	requireMiddleware(t, platformEventDeleteResponse, "platform", "platform-events")

	platformProfileDeleteResponse := performJSONRequest(t, app, http.MethodDelete, "/platform/profile", nil)
	requireStatus(t, platformProfileDeleteResponse, http.StatusNoContent)
	requireMiddleware(t, platformProfileDeleteResponse, "platform", "platform-profile")

	settingDeleteResponse := performJSONRequest(t, app, http.MethodDelete, "/platform", nil)
	requireStatus(t, settingDeleteResponse, http.StatusNoContent)
	requireMiddleware(t, settingDeleteResponse, "platform")

	missingSettingResponse := performJSONRequest(t, app, http.MethodGet, "/platform", nil)
	requireStatus(t, missingSettingResponse, http.StatusNotFound)
	requireMiddleware(t, missingSettingResponse, "platform")
}

func integrationMiddleware(name string) services.MiddlewareFunc {
	return func(next services.HandlerFunc) services.HandlerFunc {
		return func(context services.Context) error {
			if native, ok := context.Native().(echov4.Context); ok {
				native.Response().Header().Add("X-Integration-Middleware", name)
			}
			return next(context)
		}
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

func performJSONRequest(t *testing.T, app *echov4.Echo, method string, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return performJSONRequestWithHeaders(t, app, method, target, body, nil)
}

func performJSONRequestWithHeaders(t *testing.T, app *echov4.Echo, method string, target string, body any, headers map[string]string) *httptest.ResponseRecorder {
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
	app.ServeHTTP(response, request)
	return response
}

func requireStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()

	if response.Code != status {
		t.Fatalf("expected status %d, got %d with body %s", status, response.Code, response.Body.String())
	}
}

func requireMiddleware(t *testing.T, response *httptest.ResponseRecorder, names ...string) {
	t.Helper()

	values := response.Header().Values("X-Integration-Middleware")
	for _, name := range names {
		found := false
		for _, value := range values {
			if value == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected middleware %q in %v; status=%d body=%s", name, values, response.Code, response.Body.String())
		}
	}
}

func decodeJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()

	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v; response status=%d body=%s", err, response.Code, response.Body.String())
	}
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
