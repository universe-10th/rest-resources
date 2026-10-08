package services

import (
	"encoding"
	"reflect"
	"strconv"
)

// CookieSameSite describes the SameSite policy for a response cookie.
type CookieSameSite uint8

// EndpointType describes what kind of endpoint is being registered.
// Either standard (verb), collection action, or element action.
type EndpointType uint8

const (
	// CookieSameSiteDefault leaves SameSite unspecified.
	CookieSameSiteDefault CookieSameSite = iota

	// CookieSameSiteLax maps to the common Lax SameSite policy.
	CookieSameSiteLax

	// CookieSameSiteStrict maps to the common Strict SameSite policy.
	CookieSameSiteStrict

	// CookieSameSiteNone maps to the common None SameSite policy.
	CookieSameSiteNone
)

const (
	// EndpointVerb means a standard implemented endpoint for one
	// of the available verbs.
	EndpointVerb EndpointType = iota

	// EndpointCollectionExtra means an endpoint with collection-wide
	// logic. It does not, actually, impose restrictions other than the
	// fact that the resource is referenced.
	EndpointCollectionExtra

	// EndpointElementExtra means an endpoint with element-wide logic.
	// The restriction here is that an element must be found for this
	// logic to be accessible. It's
	EndpointElementExtra
)

// Cookie is a portable cookie representation that can be mapped to common Go
// web frameworks without exposing one framework's native cookie type.
type Cookie struct {
	Name     string
	Value    string
	Path     string
	Domain   string
	MaxAge   int
	Secure   bool
	HTTPOnly bool
	SameSite CookieSameSite
}

// Context is an abstract context over any supported library.
// It is, typically, a wrapper over a request context.
type Context interface {
	// Native gets the underlying native context, according
	// to what the underlying library supports.
	Native() any

	// GetPathParam gets a param from the URL path.
	GetPathParam(string) (string, error)

	// GetQueryParam gets the first value of a query-string parameter.
	GetQueryParam(string) (string, error)

	// GetQueryParams gets all values of a query-string parameter.
	GetQueryParams(string) ([]string, error)

	// GetHeader gets the first value of a request header.
	GetHeader(string) (string, error)

	// GetHeaders gets all values of a request header.
	GetHeaders(string) ([]string, error)

	// GetCookie gets a request cookie by name.
	GetCookie(string) (Cookie, error)

	// BindJSON binds the JSON request body into target.
	BindJSON(target any) error

	// SetHeader sets a response header value.
	SetHeader(name string, value string)

	// SetCookie adds a response cookie.
	SetCookie(cookie Cookie)

	// ResponseAlreadySent tells whether the response has already been sent.
	ResponseAlreadySent() bool

	// GetData retrieves arbitrary data for this context,
	// typically stored by middleware functions.
	GetData(name string) (any, bool)

	// SetData sets arbitrary data for this context. Typically
	// used by middleware functions.
	SetData(name string, value any)

	// PushElement pushes an element in the context stack.
	// By convention, the element should always be a pointer
	// to a resource.
	PushElement(resource any)

	// PopElement pops an element from the context stack.
	// By convention, the element should always be a pointer
	// to a resource. The second argument will be false if
	// there are no elements to pop.
	PopElement() (any, bool)

	// PeekElement peeks any one element, without popping it,
	// from the context stack. By convention, the element should
	// always be a pointer to a resource. The second argument
	// will be false if there are no elements to pop.
	PeekElement(index int) (any, bool)

	// RenderJSON renders body as a JSON response with status.
	RenderJSON(status int, body any) error

	// RenderNoContent renders a response status without a body.
	RenderNoContent(status int) error

	// CurrentService tells which resource is the one attending
	// the request. Useful for custom logic endpoints.
	CurrentService() any

	// CurrentEndpoint tells which is the current endpoint being
	// accessed. The first argument tells the type of endpoint.
	// The second argument tells which standard verb (if the type
	// is EndpointVerb). The third argument tells which name of
	// the custom endpoint is used (if the type is not EndpointVerb).
	CurrentEndpoint() (EndpointType, ResourceVerb, string)

	// Setup configured the context with all the required data:
	// resource, type of endpoint, verb and name (when required).
	Setup(resource any, endpointType EndpointType, verb ResourceVerb, name string)
}

// ParsePathParam parses a path parameter string into one of the supported path
// parameter scalar types.
func ParsePathParam[T comparable](v string) (T, error) {
	var zero T
	valueType := reflect.TypeFor[T]()

	switch valueType.Kind() {
	case reflect.String:
		return reflect.ValueOf(v).Convert(valueType).Interface().(T), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(v, 10, valueType.Bits())
		if err != nil {
			return zero, err
		}

		return reflect.ValueOf(parsed).Convert(valueType).Interface().(T), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(v, 10, valueType.Bits())
		if err != nil {
			return zero, err
		}

		return reflect.ValueOf(parsed).Convert(valueType).Interface().(T), nil
	case reflect.Bool:
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return zero, err
		}

		return reflect.ValueOf(parsed).Convert(valueType).Interface().(T), nil
	default:
		parsed := zero
		unmarshaler, ok := any(&parsed).(encoding.TextUnmarshaler)
		if !ok {
			return zero, strconv.ErrSyntax
		}
		if err := unmarshaler.UnmarshalText([]byte(v)); err != nil {
			return zero, err
		}

		return parsed, nil
	}
}
