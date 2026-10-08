package echo

import (
	"errors"
	"net/http"
	"reflect"

	echov4 "github.com/labstack/echo/v4"
	"github.com/universe-10th/rest-resources/types/services"
	"github.com/universe-10th/rest-resources/utils"
)

var (
	ErrInvalidEchoApp       = errors.New("invalid echo app")
	ErrInvalidService       = errors.New("invalid service")
	ErrInvalidRootService   = errors.New("invalid root service")
	echoContextEnvelopeKey  = "__echo_resources_context"
	echoContextUserDataPref = "__echo_resources_data_"
)

// GroupProvider is an Echo app or group that can create child groups.
type GroupProvider interface {
	Group(prefix string, m ...echov4.MiddlewareFunc) *echov4.Group
}

// Context wraps an Echo context with the framework-neutral services.Context API.
type Context struct {
	context      echov4.Context
	stack        []any
	service      any
	endpointType services.EndpointType
	verb         services.ResourceVerb
	name         string
}

// WrapContext returns the request-scoped services.Context envelope for c.
func WrapContext(c echov4.Context) *Context {
	if wrapped, ok := c.Get(echoContextEnvelopeKey).(*Context); ok {
		return wrapped
	}

	wrapped := &Context{context: c}
	c.Set(echoContextEnvelopeKey, wrapped)
	return wrapped
}

func (context *Context) Native() any {
	return context.context
}

func (context *Context) GetPathParam(name string) (string, error) {
	for _, paramName := range context.context.ParamNames() {
		if paramName == name {
			return context.context.Param(name), nil
		}
	}

	return "", echov4.ErrNotFound
}

func (context *Context) GetQueryParam(name string) (string, error) {
	values, ok := context.context.QueryParams()[name]
	if !ok || len(values) == 0 {
		return "", echov4.ErrNotFound
	}

	return values[0], nil
}

func (context *Context) GetQueryParams(name string) ([]string, error) {
	values, ok := context.context.QueryParams()[name]
	if !ok {
		return nil, echov4.ErrNotFound
	}

	copy_ := make([]string, len(values))
	copy(copy_, values)
	return copy_, nil
}

func (context *Context) GetHeader(name string) (string, error) {
	values := context.context.Request().Header.Values(name)
	if len(values) == 0 {
		return "", echov4.ErrNotFound
	}

	return values[0], nil
}

func (context *Context) GetHeaders(name string) ([]string, error) {
	values := context.context.Request().Header.Values(name)
	if len(values) == 0 {
		return nil, echov4.ErrNotFound
	}

	copy_ := make([]string, len(values))
	copy(copy_, values)
	return copy_, nil
}

func (context *Context) GetCookie(name string) (services.Cookie, error) {
	cookie, err := context.context.Cookie(name)
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

func (context *Context) BindJSON(target any) error {
	return (&echov4.DefaultBinder{}).BindBody(context.context, target)
}

func (context *Context) SetHeader(name string, value string) {
	context.context.Response().Header().Set(name, value)
}

func (context *Context) SetCookie(cookie services.Cookie) {
	context.context.SetCookie(&http.Cookie{
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

func (context *Context) ResponseAlreadySent() bool {
	return context.context.Response().Committed
}

func (context *Context) GetData(name string) (any, bool) {
	value := context.context.Get(echoContextUserDataPref + name)
	return value, value != nil
}

func (context *Context) SetData(name string, value any) {
	context.context.Set(echoContextUserDataPref+name, value)
}

func (context *Context) PushElement(resource any) {
	context.stack = append(context.stack, resource)
}

func (context *Context) PopElement() (any, bool) {
	if len(context.stack) == 0 {
		return nil, false
	}

	index := len(context.stack) - 1
	resource := context.stack[index]
	context.stack = context.stack[:index]
	return resource, true
}

func (context *Context) PeekElement(index int) (any, bool) {
	if index < 0 || index >= len(context.stack) {
		return nil, false
	}

	return context.stack[len(context.stack)-1-index], true
}

func (context *Context) RenderJSON(status int, body any) error {
	return context.context.JSON(status, body)
}

func (context *Context) RenderNoContent(status int) error {
	return context.context.NoContent(status)
}

func (context *Context) CurrentService() any {
	return context.service
}

func (context *Context) CurrentEndpoint() (services.EndpointType, services.ResourceVerb, string) {
	return context.endpointType, context.verb, context.name
}

func (context *Context) Setup(
	service any,
	endpointType services.EndpointType,
	verb services.ResourceVerb,
	name string,
) {
	context.service = service
	context.endpointType = endpointType
	context.verb = verb
	context.name = name
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

// MustInstall installs a non-child service inside an Echo app or group.
// Panics if the echo app or group is nil, the service is nil or the
// service has a parent.
func MustInstall(app GroupProvider, service services.Service) {
	if isNilGroupProvider(app) {
		panic(ErrInvalidEchoApp)
	}
	if service == nil {
		panic(ErrInvalidService)
	}
	if service.Parent() != nil {
		panic(ErrInvalidRootService)
	}

	installService(app.Group(""), service)
}

// Install installs a non-child service inside an Echo app or group.
// Returns an error if the echo app or group is nil, the service is
// nil or the service has a parent.
func Install(app GroupProvider, service services.Service) (err error) {
	defer func() {
		if v := recover(); v != nil {
			if err_, ok := v.(error); ok {
				err = err_
			} else {
				panic(v)
			}
		}
	}()

	MustInstall(app, service)
	return nil
}

func isNilGroupProvider(app GroupProvider) bool {
	if app == nil {
		return true
	}

	value := reflect.ValueOf(app)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func installService(base *echov4.Group, service services.Service) {
	group := base.Group("/" + service.Prefix())
	verbs := service.Verbs()

	if service.IsSingleton() {
		installSingleton(group, service, verbs)
	} else {
		installCollection(group, service, verbs)
	}
}

func installSingleton(group *echov4.Group, service services.Service, verbs utils.Flags[services.ResourceVerb]) {
	liveElementGroup := group.Group("", endpointMiddlewares(service, services.ResourceGet, service.ElementMiddleware(false))...)

	if verbs.Has(services.ResourceCreate) {
		group.POST("", wrapHandler(service.Create), endpointMiddlewares(service, services.ResourceCreate)...)
	}
	if verbs.Has(services.ResourceGet) {
		liveElementGroup.GET("", wrapHandler(service.Get))
	}
	if verbs.Has(services.ResourceUpdate) {
		group.PATCH("", wrapHandler(service.Update), endpointMiddlewares(service, services.ResourceUpdate, service.ElementMiddleware(false))...)
	}
	if verbs.Has(services.ResourceDelete) {
		group.DELETE("", wrapHandler(service.Delete), endpointMiddlewares(service, services.ResourceDelete, service.ElementMiddleware(false))...)
	}

	if service.IsSoftDeleted() {
		installDeletedSingleton(group, service, verbs)
	}

	installElementExtras(group, "", service)
	installChildren(liveElementGroup, service)
}

func installCollection(group *echov4.Group, service services.Service, verbs utils.Flags[services.ResourceVerb]) {
	liveElementPath := "/:" + service.URLArg()
	liveElementGroup := group.Group(liveElementPath, endpointMiddlewares(service, services.ResourceGet, service.ElementMiddleware(false))...)

	if verbs.Has(services.ResourceList) {
		group.GET("", wrapListHandler(service, false), endpointMiddlewares(service, services.ResourceList)...)
	}
	if verbs.Has(services.ResourceCreate) {
		group.POST("", wrapHandler(service.Create), endpointMiddlewares(service, services.ResourceCreate)...)
	}
	if verbs.Has(services.ResourceGet) {
		liveElementGroup.GET("", wrapHandler(service.Get))
	}
	if verbs.Has(services.ResourceUpdate) {
		group.PATCH(liveElementPath, wrapHandler(service.Update), endpointMiddlewares(service, services.ResourceUpdate, service.ElementMiddleware(false))...)
	}
	if verbs.Has(services.ResourceDelete) {
		group.DELETE(liveElementPath, wrapHandler(service.Delete), endpointMiddlewares(service, services.ResourceDelete, service.ElementMiddleware(false))...)
	}

	if service.IsSoftDeleted() {
		installDeletedCollection(group, service, verbs)
	}

	installCollectionExtras(group, service)
	installElementExtras(group, liveElementPath, service)
	installChildren(liveElementGroup, service)
}

func installDeletedSingleton(group *echov4.Group, service services.Service, verbs utils.Flags[services.ResourceVerb]) {
	deletedGroup := group.Group("/deleted")

	if verbs.Has(services.ResourceGetDeleted) {
		deletedGroup.GET("", wrapHandler(service.Get), endpointMiddlewares(service, services.ResourceGetDeleted, service.ElementMiddleware(true))...)
	}
	if verbs.Has(services.ResourceRestore) {
		deletedGroup.POST("", wrapHandler(service.Restore), endpointMiddlewares(service, services.ResourceRestore, service.ElementMiddleware(true))...)
	}
	if verbs.Has(services.ResourcePrune) {
		deletedGroup.DELETE("", wrapHandler(service.Prune), endpointMiddlewares(service, services.ResourcePrune, service.ElementMiddleware(true))...)
	}
}

func installDeletedCollection(group *echov4.Group, service services.Service, verbs utils.Flags[services.ResourceVerb]) {
	deletedGroup := group.Group("/deleted")
	deletedElementPath := "/:" + service.URLArg()

	if verbs.Has(services.ResourceListDeleted) {
		deletedGroup.GET("", wrapListHandler(service, true), endpointMiddlewares(service, services.ResourceListDeleted)...)
	}
	if verbs.Has(services.ResourceGetDeleted) {
		deletedGroup.GET(deletedElementPath, wrapHandler(service.Get), endpointMiddlewares(service, services.ResourceGetDeleted, service.ElementMiddleware(true))...)
	}
	if verbs.Has(services.ResourceRestore) {
		deletedGroup.POST(deletedElementPath, wrapHandler(service.Restore), endpointMiddlewares(service, services.ResourceRestore, service.ElementMiddleware(true))...)
	}
	if verbs.Has(services.ResourcePrune) {
		deletedGroup.DELETE(deletedElementPath, wrapHandler(service.Prune), endpointMiddlewares(service, services.ResourcePrune, service.ElementMiddleware(true))...)
	}
}

func installChildren(base *echov4.Group, service services.Service) {
	if !service.CanHaveChildren() {
		return
	}

	for _, child := range service.Children() {
		installService(base, child)
	}
}

func installCollectionExtras(group *echov4.Group, service services.Service) {
	for _, endpoint := range service.CollectionExtras() {
		group.Add(
			endpoint.Method,
			"/"+endpoint.Name,
			wrapHandler(endpoint.Handler),
			extraEndpointMiddlewares(service, services.EndpointCollectionExtra, endpoint.Name)...,
		)
	}
}

func installElementExtras(group *echov4.Group, elementPath string, service services.Service) {
	for _, endpoint := range service.ElementExtras() {
		group.Add(
			endpoint.Method,
			elementPath+"/"+endpoint.Name,
			wrapHandler(endpoint.Handler),
			extraEndpointMiddlewares(
				service,
				services.EndpointElementExtra,
				endpoint.Name,
				service.ElementMiddleware(false),
			)...,
		)
	}
}

func endpointMiddlewares(
	service services.Service,
	verb services.ResourceVerb,
	extra ...services.MiddlewareFunc,
) []echov4.MiddlewareFunc {
	middlewares := []services.MiddlewareFunc{
		setupMiddleware(service, services.EndpointVerb, verb, ""),
	}
	return wrapEndpointMiddlewares(middlewares, service, extra...)
}

func extraEndpointMiddlewares(
	service services.Service,
	endpointType services.EndpointType,
	name string,
	extra ...services.MiddlewareFunc,
) []echov4.MiddlewareFunc {
	middlewares := []services.MiddlewareFunc{
		setupMiddleware(service, endpointType, 0, name),
	}
	return wrapEndpointMiddlewares(middlewares, service, extra...)
}

func wrapEndpointMiddlewares(
	middlewares []services.MiddlewareFunc,
	service services.Service,
	extra ...services.MiddlewareFunc,
) []echov4.MiddlewareFunc {
	middlewares = append(middlewares, service.Middlewares()...)
	middlewares = append(middlewares, extra...)

	wrapped := make([]echov4.MiddlewareFunc, 0, len(middlewares))
	for _, middleware := range middlewares {
		wrapped = append(wrapped, wrapMiddleware(middleware))
	}
	return wrapped
}

func setupMiddleware(
	service services.Service,
	endpointType services.EndpointType,
	verb services.ResourceVerb,
	name string,
) services.MiddlewareFunc {
	return func(next services.HandlerFunc) services.HandlerFunc {
		return func(context services.Context) error {
			context.Setup(service, endpointType, verb, name)
			return next(context)
		}
	}
}

func wrapHandler(handler services.HandlerFunc) echov4.HandlerFunc {
	return func(context echov4.Context) error {
		return handler(WrapContext(context))
	}
}

func wrapListHandler(service services.Service, deleted bool) echov4.HandlerFunc {
	return wrapHandler(func(context services.Context) error {
		return service.List(context, deleted)
	})
}

func wrapMiddleware(middleware services.MiddlewareFunc) echov4.MiddlewareFunc {
	return func(next echov4.HandlerFunc) echov4.HandlerFunc {
		return func(context echov4.Context) error {
			wrapped := WrapContext(context)
			handler := middleware(func(services.Context) error {
				return next(context)
			})
			return handler(wrapped)
		}
	}
}
