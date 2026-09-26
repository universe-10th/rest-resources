package nethttpadapter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	chiv5 "github.com/go-chi/chi/v5"
	"github.com/universe-10th/rest-resources/types/services"
	"github.com/universe-10th/rest-resources/utils"
)

type Router interface {
	Route(pattern string, fn func(r chiv5.Router)) chiv5.Router
	MethodFunc(method, pattern string, handlerFn http.HandlerFunc)
}

var (
	ErrInvalidRouter      = errors.New("invalid router")
	ErrInvalidService     = errors.New("invalid service")
	ErrInvalidRootService = errors.New("invalid root service")
)

type contextKey struct{}

type Context struct {
	response     http.ResponseWriter
	request      *http.Request
	stack        []any
	data         map[string]any
	service      any
	endpointType services.EndpointType
	verb         services.ResourceVerb
	name         string
}

func WrapContext(response http.ResponseWriter, request *http.Request) *Context {
	if wrapped, ok := request.Context().Value(contextKey{}).(*Context); ok {
		wrapped.response = response
		wrapped.request = request
		return wrapped
	}

	wrapped := &Context{response: response, request: request}
	*request = *request.WithContext(context.WithValue(request.Context(), contextKey{}, wrapped))
	return wrapped
}

func (c *Context) Native() any {
	return c.request
}

func (c *Context) GetPathParam(name string) (string, error) {
	value := chiv5.URLParam(c.request, name)
	if value == "" {
		return "", http.ErrMissingFile
	}
	return value, nil
}

func (c *Context) GetQueryParam(name string) (string, error) {
	values, ok := c.request.URL.Query()[name]
	if !ok || len(values) == 0 {
		return "", http.ErrMissingFile
	}
	return values[0], nil
}

func (c *Context) GetQueryParams(name string) ([]string, error) {
	values, ok := c.request.URL.Query()[name]
	if !ok {
		return nil, http.ErrMissingFile
	}
	copy_ := make([]string, len(values))
	copy(copy_, values)
	return copy_, nil
}

func (c *Context) GetHeader(name string) (string, error) {
	values := c.request.Header.Values(name)
	if len(values) == 0 {
		return "", http.ErrMissingFile
	}
	return values[0], nil
}

func (c *Context) GetHeaders(name string) ([]string, error) {
	values := c.request.Header.Values(name)
	if len(values) == 0 {
		return nil, http.ErrMissingFile
	}
	copy_ := make([]string, len(values))
	copy(copy_, values)
	return copy_, nil
}

func (c *Context) GetCookie(name string) (services.Cookie, error) {
	cookie, err := c.request.Cookie(name)
	if err != nil {
		return services.Cookie{}, err
	}
	return services.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		Path:     cookie.Path,
		Domain:   cookie.Domain,
		MaxAge:   cookie.MaxAge,
		Secure:   cookie.Secure,
		HTTPOnly: cookie.HttpOnly,
		SameSite: fromHTTPSameSite(cookie.SameSite),
	}, nil
}

func (c *Context) BindJSON(target any) error {
	return json.NewDecoder(c.request.Body).Decode(target)
}

func (c *Context) SetHeader(name string, value string) {
	c.response.Header().Set(name, value)
}

func (c *Context) SetCookie(cookie services.Cookie) {
	http.SetCookie(c.response, &http.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		Path:     cookie.Path,
		Domain:   cookie.Domain,
		MaxAge:   cookie.MaxAge,
		Secure:   cookie.Secure,
		HttpOnly: cookie.HTTPOnly,
		SameSite: toHTTPSameSite(cookie.SameSite),
	})
}

func (c *Context) GetData(name string) (any, bool) {
	if c.data == nil {
		return nil, false
	}
	value, ok := c.data[name]
	return value, ok
}

func (c *Context) SetData(name string, value any) {
	if c.data == nil {
		c.data = map[string]any{}
	}
	c.data[name] = value
}

func (c *Context) PushElement(resource any) {
	c.stack = append(c.stack, resource)
}

func (c *Context) PopElement() (any, bool) {
	if len(c.stack) == 0 {
		return nil, false
	}
	index := len(c.stack) - 1
	resource := c.stack[index]
	c.stack = c.stack[:index]
	return resource, true
}

func (c *Context) PeekElement(index int) (any, bool) {
	if index < 0 || index >= len(c.stack) {
		return nil, false
	}
	return c.stack[len(c.stack)-1-index], true
}

func (c *Context) RenderJSON(status int, body any) error {
	c.response.Header().Set("Content-Type", "application/json")
	c.response.WriteHeader(status)
	return json.NewEncoder(c.response).Encode(body)
}

func (c *Context) RenderNoContent(status int) error {
	c.response.WriteHeader(status)
	return nil
}

func (c *Context) CurrentService() any {
	return c.service
}

func (c *Context) CurrentEndpoint() (services.EndpointType, services.ResourceVerb, string) {
	return c.endpointType, c.verb, c.name
}

func (c *Context) Setup(service any, endpointType services.EndpointType, verb services.ResourceVerb, name string) {
	c.service = service
	c.endpointType = endpointType
	c.verb = verb
	c.name = name
}

func fromHTTPSameSite(sameSite http.SameSite) services.CookieSameSite {
	switch sameSite {
	case http.SameSiteLaxMode:
		return services.CookieSameSiteLax
	case http.SameSiteStrictMode:
		return services.CookieSameSiteStrict
	case http.SameSiteNoneMode:
		return services.CookieSameSiteNone
	default:
		return services.CookieSameSiteDefault
	}
}

func toHTTPSameSite(sameSite services.CookieSameSite) http.SameSite {
	switch sameSite {
	case services.CookieSameSiteLax:
		return http.SameSiteLaxMode
	case services.CookieSameSiteStrict:
		return http.SameSiteStrictMode
	case services.CookieSameSiteNone:
		return http.SameSiteNoneMode
	default:
		return http.SameSiteDefaultMode
	}
}

func MustInstall(router Router, service services.Service) {
	if isNilRouter(router) {
		panic(ErrInvalidRouter)
	}
	if service == nil {
		panic(ErrInvalidService)
	}
	if service.Parent() != nil {
		panic(ErrInvalidRootService)
	}

	installService(router, service)
}

func Install(router Router, service services.Service) (err error) {
	defer func() {
		if v := recover(); v != nil {
			if err_, ok := v.(error); ok {
				err = err_
			} else {
				panic(v)
			}
		}
	}()

	MustInstall(router, service)
	return nil
}

func isNilRouter(router Router) bool {
	if router == nil {
		return true
	}

	value := reflect.ValueOf(router)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func installService(base Router, service services.Service) {
	base.Route("/"+service.Prefix(), func(group chiv5.Router) {
		verbs := service.Verbs()
		if service.IsSingleton() {
			installSingleton(group, service, verbs)
		} else {
			installCollection(group, service, verbs)
		}
	})
}

func installSingleton(group Router, service services.Service, verbs utils.Flags[services.ResourceVerb]) {
	if verbs.Has(services.ResourceCreate) {
		group.MethodFunc(http.MethodPost, "/", handler(service, services.EndpointVerb, services.ResourceCreate, "", nil, service.Create))
	}
	if verbs.Has(services.ResourceGet) {
		group.MethodFunc(http.MethodGet, "/", handler(service, services.EndpointVerb, services.ResourceGet, "", []services.MiddlewareFunc{service.ElementMiddleware(false)}, service.Get))
	}
	if verbs.Has(services.ResourceUpdate) {
		group.MethodFunc(http.MethodPatch, "/", handler(service, services.EndpointVerb, services.ResourceUpdate, "", []services.MiddlewareFunc{service.ElementMiddleware(false)}, service.Update))
	}
	if verbs.Has(services.ResourceDelete) {
		group.MethodFunc(http.MethodDelete, "/", handler(service, services.EndpointVerb, services.ResourceDelete, "", []services.MiddlewareFunc{service.ElementMiddleware(false)}, service.Delete))
	}

	if service.IsSoftDeleted() {
		installDeletedSingleton(group, service, verbs)
	}

	installElementExtras(group, "", service)
	installChildren(group, service, "")
}

func installCollection(group Router, service services.Service, verbs utils.Flags[services.ResourceVerb]) {
	liveElementPath := "/{" + service.URLArg() + "}"

	if verbs.Has(services.ResourceList) {
		group.MethodFunc(http.MethodGet, "/", listHandler(service, services.ResourceList, false, nil))
	}
	if verbs.Has(services.ResourceCreate) {
		group.MethodFunc(http.MethodPost, "/", handler(service, services.EndpointVerb, services.ResourceCreate, "", nil, service.Create))
	}
	installCollectionExtras(group, service)
	if service.IsSoftDeleted() {
		installDeletedCollection(group, service, verbs)
	}
	if verbs.Has(services.ResourceGet) {
		group.MethodFunc(http.MethodGet, liveElementPath, handler(service, services.EndpointVerb, services.ResourceGet, "", []services.MiddlewareFunc{service.ElementMiddleware(false)}, service.Get))
	}
	if verbs.Has(services.ResourceUpdate) {
		group.MethodFunc(http.MethodPatch, liveElementPath, handler(service, services.EndpointVerb, services.ResourceUpdate, "", []services.MiddlewareFunc{service.ElementMiddleware(false)}, service.Update))
	}
	if verbs.Has(services.ResourceDelete) {
		group.MethodFunc(http.MethodDelete, liveElementPath, handler(service, services.EndpointVerb, services.ResourceDelete, "", []services.MiddlewareFunc{service.ElementMiddleware(false)}, service.Delete))
	}

	installElementExtras(group, liveElementPath, service)
	installChildren(group, service, liveElementPath)
}

func installDeletedSingleton(group Router, service services.Service, verbs utils.Flags[services.ResourceVerb]) {
	if verbs.Has(services.ResourceGetDeleted) {
		group.MethodFunc(http.MethodGet, "/deleted", handler(service, services.EndpointVerb, services.ResourceGetDeleted, "", []services.MiddlewareFunc{service.ElementMiddleware(true)}, service.Get))
	}
	if verbs.Has(services.ResourceRestore) {
		group.MethodFunc(http.MethodPost, "/deleted", handler(service, services.EndpointVerb, services.ResourceRestore, "", []services.MiddlewareFunc{service.ElementMiddleware(true)}, service.Restore))
	}
	if verbs.Has(services.ResourcePrune) {
		group.MethodFunc(http.MethodDelete, "/deleted", handler(service, services.EndpointVerb, services.ResourcePrune, "", []services.MiddlewareFunc{service.ElementMiddleware(true)}, service.Prune))
	}
}

func installDeletedCollection(group Router, service services.Service, verbs utils.Flags[services.ResourceVerb]) {
	deletedElementPath := "/deleted/{" + service.URLArg() + "}"

	if verbs.Has(services.ResourceListDeleted) {
		group.MethodFunc(http.MethodGet, "/deleted", listHandler(service, services.ResourceListDeleted, true, nil))
	}
	if verbs.Has(services.ResourceGetDeleted) {
		group.MethodFunc(http.MethodGet, deletedElementPath, handler(service, services.EndpointVerb, services.ResourceGetDeleted, "", []services.MiddlewareFunc{service.ElementMiddleware(true)}, service.Get))
	}
	if verbs.Has(services.ResourceRestore) {
		group.MethodFunc(http.MethodPost, deletedElementPath, handler(service, services.EndpointVerb, services.ResourceRestore, "", []services.MiddlewareFunc{service.ElementMiddleware(true)}, service.Restore))
	}
	if verbs.Has(services.ResourcePrune) {
		group.MethodFunc(http.MethodDelete, deletedElementPath, handler(service, services.EndpointVerb, services.ResourcePrune, "", []services.MiddlewareFunc{service.ElementMiddleware(true)}, service.Prune))
	}
}

func installCollectionExtras(group Router, service services.Service) {
	for _, endpoint := range service.CollectionExtras() {
		group.MethodFunc(endpoint.Method, "/"+endpoint.Name, handler(service, services.EndpointCollectionExtra, 0, endpoint.Name, nil, endpoint.Handler))
	}
}

func installElementExtras(group Router, elementPath string, service services.Service) {
	for _, endpoint := range service.ElementExtras() {
		group.MethodFunc(endpoint.Method, elementPath+"/"+endpoint.Name, handler(
			service,
			services.EndpointElementExtra,
			0,
			endpoint.Name,
			[]services.MiddlewareFunc{service.ElementMiddleware(false)},
			endpoint.Handler,
		))
	}
}

func installChildren(group Router, service services.Service, elementPath string) {
	if !service.CanHaveChildren() {
		return
	}

	for _, child := range service.Children() {
		group.Route(elementPath+"/"+child.Prefix(), func(childGroup chiv5.Router) {
			childGroup.Use(httpMiddleware(
				service,
				services.EndpointVerb,
				services.ResourceGet,
				"",
				[]services.MiddlewareFunc{service.ElementMiddleware(false)},
			))
			installChildService(childGroup, child)
		})
	}
}

func installChildService(group Router, service services.Service) {
	verbs := service.Verbs()
	if service.IsSingleton() {
		installSingleton(group, service, verbs)
	} else {
		installCollection(group, service, verbs)
	}
}

func listHandler(
	service services.Service,
	verb services.ResourceVerb,
	deleted bool,
	extra []services.MiddlewareFunc,
) http.HandlerFunc {
	return handler(service, services.EndpointVerb, verb, "", extra, func(context services.Context) error {
		return service.List(context, deleted)
	})
}

func handler(
	service services.Service,
	endpointType services.EndpointType,
	verb services.ResourceVerb,
	name string,
	extra []services.MiddlewareFunc,
	handler services.HandlerFunc,
) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		wrapped := WrapContext(response, request)
		endpointHandler := applyMiddlewares(service, endpointType, verb, name, extra, handler)
		if err := endpointHandler(wrapped); err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
		}
	}
}

func httpMiddleware(
	service services.Service,
	endpointType services.EndpointType,
	verb services.ResourceVerb,
	name string,
	extra []services.MiddlewareFunc,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			wrapped := WrapContext(response, request)
			endpointHandler := applyMiddlewares(service, endpointType, verb, name, extra, func(context services.Context) error {
				next.ServeHTTP(response, request)
				return nil
			})
			if err := endpointHandler(wrapped); err != nil {
				http.Error(response, err.Error(), http.StatusInternalServerError)
			}
		})
	}
}

func applyMiddlewares(
	service services.Service,
	endpointType services.EndpointType,
	verb services.ResourceVerb,
	name string,
	extra []services.MiddlewareFunc,
	handler services.HandlerFunc,
) services.HandlerFunc {
	middlewares := []services.MiddlewareFunc{setupMiddleware(service, endpointType, verb, name)}
	middlewares = append(middlewares, service.Middlewares()...)
	middlewares = append(middlewares, extra...)

	result := handler
	for i := len(middlewares) - 1; i >= 0; i-- {
		result = middlewares[i](result)
	}
	return result
}

func setupMiddleware(service services.Service, endpointType services.EndpointType, verb services.ResourceVerb, name string) services.MiddlewareFunc {
	return func(next services.HandlerFunc) services.HandlerFunc {
		return func(context services.Context) error {
			context.Setup(service, endpointType, verb, name)
			return next(context)
		}
	}
}
