package fiber

import (
	"errors"
	"reflect"

	fiberv3 "github.com/gofiber/fiber/v3"
	"github.com/universe-10th/rest-resources/types/services"
	"github.com/universe-10th/rest-resources/utils"
)

var (
	ErrInvalidFiberApp     = errors.New("invalid fiber app")
	ErrInvalidService      = errors.New("invalid service")
	ErrInvalidRootService  = errors.New("invalid root service")
	contextEnvelopeKey     = "__rest_resources_fiber_context"
	contextUserDataKeyPref = "__rest_resources_data_"
)

type Router interface {
	Add(methods []string, path string, handler any, handlers ...any) fiberv3.Router
	Group(prefix string, handlers ...any) fiberv3.Router
}

type Context struct {
	context      fiberv3.Ctx
	stack        []any
	service      any
	endpointType services.EndpointType
	verb         services.ResourceVerb
	name         string
}

func WrapContext(c fiberv3.Ctx) *Context {
	if wrapped, ok := c.Locals(contextEnvelopeKey).(*Context); ok {
		wrapped.context = c
		return wrapped
	}

	wrapped := &Context{context: c}
	c.Locals(contextEnvelopeKey, wrapped)
	return wrapped
}

func (c *Context) Native() any {
	return c.context
}

func (c *Context) GetPathParam(name string) (string, error) {
	value := c.context.Params(name)
	if value == "" {
		return "", fiberv3.ErrNotFound
	}
	return value, nil
}

func (c *Context) GetQueryParam(name string) (string, error) {
	value := c.context.Query(name)
	if value == "" {
		return "", fiberv3.ErrNotFound
	}
	return value, nil
}

func (c *Context) GetQueryParams(name string) ([]string, error) {
	values := c.context.Request().URI().QueryArgs().PeekMulti(name)
	if len(values) == 0 {
		return nil, fiberv3.ErrNotFound
	}
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result, nil
}

func (c *Context) GetHeader(name string) (string, error) {
	value := c.context.Get(name)
	if value == "" {
		return "", fiberv3.ErrNotFound
	}
	return value, nil
}

func (c *Context) GetHeaders(name string) ([]string, error) {
	values := c.context.GetReqHeaders()[name]
	if len(values) == 0 {
		return nil, fiberv3.ErrNotFound
	}
	copy_ := make([]string, len(values))
	copy(copy_, values)
	return copy_, nil
}

func (c *Context) GetCookie(name string) (services.Cookie, error) {
	value := c.context.Cookies(name)
	if value == "" {
		return services.Cookie{}, fiberv3.ErrNotFound
	}
	return services.Cookie{Name: name, Value: value}, nil
}

func (c *Context) BindJSON(target any) error {
	return c.context.Bind().Body(target)
}

func (c *Context) SetHeader(name string, value string) {
	c.context.Set(name, value)
}

func (c *Context) SetCookie(cookie services.Cookie) {
	c.context.Cookie(&fiberv3.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		Path:     cookie.Path,
		Domain:   cookie.Domain,
		MaxAge:   cookie.MaxAge,
		Secure:   cookie.Secure,
		HTTPOnly: cookie.HTTPOnly,
		SameSite: toFiberSameSite(cookie.SameSite),
	})
}

func (c *Context) GetData(name string) (any, bool) {
	value := c.context.Locals(contextUserDataKeyPref + name)
	return value, value != nil
}

func (c *Context) SetData(name string, value any) {
	c.context.Locals(contextUserDataKeyPref+name, value)
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
	return c.context.Status(status).JSON(body)
}

func (c *Context) RenderNoContent(status int) error {
	return c.context.SendStatus(status)
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

func toFiberSameSite(sameSite services.CookieSameSite) string {
	switch sameSite {
	case services.CookieSameSiteLax:
		return "Lax"
	case services.CookieSameSiteStrict:
		return "Strict"
	case services.CookieSameSiteNone:
		return "None"
	default:
		return ""
	}
}

func MustInstall(app Router, service services.Service) {
	if isNilRouter(app) {
		panic(ErrInvalidFiberApp)
	}
	if service == nil {
		panic(ErrInvalidService)
	}
	if service.Parent() != nil {
		panic(ErrInvalidRootService)
	}

	installService(app, service)
}

func Install(app Router, service services.Service) (err error) {
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

func isNilRouter(app Router) bool {
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

func installService(base Router, service services.Service) {
	group := base.Group("/" + service.Prefix())
	verbs := service.Verbs()
	if service.IsSingleton() {
		installSingleton(group, service, verbs)
	} else {
		installCollection(group, service, verbs)
	}
}

func installSingleton(group Router, service services.Service, verbs utils.Flags[services.ResourceVerb]) {
	if verbs.Has(services.ResourceCreate) {
		group.Add([]string{fiberv3.MethodPost}, "/", handler(service, services.EndpointVerb, services.ResourceCreate, "", nil, service.Create))
	}
	if verbs.Has(services.ResourceGet) {
		group.Add([]string{fiberv3.MethodGet}, "/", handler(service, services.EndpointVerb, services.ResourceGet, "", []services.MiddlewareFunc{service.ElementMiddleware(false)}, service.Get))
	}
	if verbs.Has(services.ResourceUpdate) {
		group.Add([]string{fiberv3.MethodPatch}, "/", handler(service, services.EndpointVerb, services.ResourceUpdate, "", []services.MiddlewareFunc{service.ElementMiddleware(false)}, service.Update))
	}
	if verbs.Has(services.ResourceDelete) {
		group.Add([]string{fiberv3.MethodDelete}, "/", handler(service, services.EndpointVerb, services.ResourceDelete, "", []services.MiddlewareFunc{service.ElementMiddleware(false)}, service.Delete))
	}

	if service.IsSoftDeleted() {
		installDeletedSingleton(group, service, verbs)
	}

	installElementExtras(group, "", service)
	installChildren(group, service, "")
}

func installCollection(group Router, service services.Service, verbs utils.Flags[services.ResourceVerb]) {
	liveElementPath := "/:" + service.URLArg()

	if verbs.Has(services.ResourceList) {
		group.Add([]string{fiberv3.MethodGet}, "/", listHandler(service, services.ResourceList, false, nil))
	}
	if verbs.Has(services.ResourceCreate) {
		group.Add([]string{fiberv3.MethodPost}, "/", handler(service, services.EndpointVerb, services.ResourceCreate, "", nil, service.Create))
	}
	installCollectionExtras(group, service)
	if service.IsSoftDeleted() {
		installDeletedCollection(group, service, verbs)
	}
	if verbs.Has(services.ResourceGet) {
		group.Add([]string{fiberv3.MethodGet}, liveElementPath, handler(service, services.EndpointVerb, services.ResourceGet, "", []services.MiddlewareFunc{service.ElementMiddleware(false)}, service.Get))
	}
	if verbs.Has(services.ResourceUpdate) {
		group.Add([]string{fiberv3.MethodPatch}, liveElementPath, handler(service, services.EndpointVerb, services.ResourceUpdate, "", []services.MiddlewareFunc{service.ElementMiddleware(false)}, service.Update))
	}
	if verbs.Has(services.ResourceDelete) {
		group.Add([]string{fiberv3.MethodDelete}, liveElementPath, handler(service, services.EndpointVerb, services.ResourceDelete, "", []services.MiddlewareFunc{service.ElementMiddleware(false)}, service.Delete))
	}

	installElementExtras(group, liveElementPath, service)
	installChildren(group, service, liveElementPath)
}

func installDeletedSingleton(group Router, service services.Service, verbs utils.Flags[services.ResourceVerb]) {
	if verbs.Has(services.ResourceGetDeleted) {
		group.Add([]string{fiberv3.MethodGet}, "/deleted", handler(service, services.EndpointVerb, services.ResourceGetDeleted, "", []services.MiddlewareFunc{service.ElementMiddleware(true)}, service.Get))
	}
	if verbs.Has(services.ResourceRestore) {
		group.Add([]string{fiberv3.MethodPost}, "/deleted", handler(service, services.EndpointVerb, services.ResourceRestore, "", []services.MiddlewareFunc{service.ElementMiddleware(true)}, service.Restore))
	}
	if verbs.Has(services.ResourcePrune) {
		group.Add([]string{fiberv3.MethodDelete}, "/deleted", handler(service, services.EndpointVerb, services.ResourcePrune, "", []services.MiddlewareFunc{service.ElementMiddleware(true)}, service.Prune))
	}
}

func installDeletedCollection(group Router, service services.Service, verbs utils.Flags[services.ResourceVerb]) {
	deletedElementPath := "/deleted/:" + service.URLArg()

	if verbs.Has(services.ResourceListDeleted) {
		group.Add([]string{fiberv3.MethodGet}, "/deleted", listHandler(service, services.ResourceListDeleted, true, nil))
	}
	if verbs.Has(services.ResourceGetDeleted) {
		group.Add([]string{fiberv3.MethodGet}, deletedElementPath, handler(service, services.EndpointVerb, services.ResourceGetDeleted, "", []services.MiddlewareFunc{service.ElementMiddleware(true)}, service.Get))
	}
	if verbs.Has(services.ResourceRestore) {
		group.Add([]string{fiberv3.MethodPost}, deletedElementPath, handler(service, services.EndpointVerb, services.ResourceRestore, "", []services.MiddlewareFunc{service.ElementMiddleware(true)}, service.Restore))
	}
	if verbs.Has(services.ResourcePrune) {
		group.Add([]string{fiberv3.MethodDelete}, deletedElementPath, handler(service, services.EndpointVerb, services.ResourcePrune, "", []services.MiddlewareFunc{service.ElementMiddleware(true)}, service.Prune))
	}
}

func installCollectionExtras(group Router, service services.Service) {
	for _, endpoint := range service.CollectionExtras() {
		group.Add([]string{endpoint.Method}, "/"+endpoint.Name, handler(service, services.EndpointCollectionExtra, 0, endpoint.Name, nil, endpoint.Handler))
	}
}

func installElementExtras(group Router, elementPath string, service services.Service) {
	for _, endpoint := range service.ElementExtras() {
		group.Add([]string{endpoint.Method}, elementPath+"/"+endpoint.Name, handler(
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
		childGroup := group.Group(
			elementPath+"/"+child.Prefix(),
			fiberMiddleware(
				service,
				services.EndpointVerb,
				services.ResourceGet,
				"",
				[]services.MiddlewareFunc{service.ElementMiddleware(false)},
			),
		)
		verbs := child.Verbs()
		if child.IsSingleton() {
			installSingleton(childGroup, child, verbs)
		} else {
			installCollection(childGroup, child, verbs)
		}
	}
}

func fiberMiddleware(
	service services.Service,
	endpointType services.EndpointType,
	verb services.ResourceVerb,
	name string,
	extra []services.MiddlewareFunc,
) fiberv3.Handler {
	return func(context fiberv3.Ctx) error {
		wrapped := WrapContext(context)
		endpointHandler := applyMiddlewares(service, endpointType, verb, name, extra, func(context services.Context) error {
			return context.Native().(fiberv3.Ctx).Next()
		})
		return endpointHandler(wrapped)
	}
}

func listHandler(
	service services.Service,
	verb services.ResourceVerb,
	deleted bool,
	extra []services.MiddlewareFunc,
) fiberv3.Handler {
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
) fiberv3.Handler {
	return func(context fiberv3.Ctx) error {
		wrapped := WrapContext(context)
		endpointHandler := applyMiddlewares(service, endpointType, verb, name, extra, handler)
		return endpointHandler(wrapped)
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
