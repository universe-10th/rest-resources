package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"reflect"
	"strconv"
	"strings"

	"github.com/universe-10th/rest-resources/types"
	"github.com/universe-10th/rest-resources/utils"
)

// ResourceVerb tells the verbs supported by the resource.
type ResourceVerb uint16

const (
	ResourceGet ResourceVerb = iota
	ResourceList
	ResourceCreate
	ResourceUpdate
	ResourceDelete
	ResourceGetDeleted
	ResourceListDeleted
	ResourceRestore
	ResourcePrune
)

var (
	ErrInvalidStorage              = errors.New("invalid storage")
	ErrInvalidParentService        = errors.New("invalid parent service")
	ErrAlreadyAttached             = errors.New("service already attached")
	ErrCyclicServiceAttachment     = errors.New("cyclic service attachment")
	ErrConflictingServiceURLArg    = errors.New("conflicting service URL arg")
	ErrInvalidConstraintJSONField  = errors.New("invalid constraint JSON field")
	ErrInvalidConstraintIDType     = errors.New("invalid constraint id type")
	ErrInvalidExtraEndpointMethod  = errors.New("invalid extra endpoint method")
	ErrInvalidExtraEndpointHandler = errors.New("invalid extra endpoint handler")
	ErrConflictingExtraEndpoint    = errors.New("conflicting extra endpoint")
	ErrCollectionExtraOnSingleton  = errors.New("collection extra endpoint on singleton")
	logger                         = slog.Default()
	defaultCollectionResourceVerbs = utils.NewFlags[ResourceVerb](
		ResourceGet, ResourceList,
		ResourceCreate, ResourceUpdate, ResourceDelete,
	)
	defaultSingletonResourceVerbs = utils.NewFlags[ResourceVerb](
		ResourceGet,
		ResourceCreate, ResourceUpdate, ResourceDelete,
	)
	defaultSoftDeletedCollectionResourceVerbs = utils.NewFlags[ResourceVerb](
		ResourceGet, ResourceList, ResourceGetDeleted, ResourceListDeleted,
		ResourceCreate, ResourceUpdate, ResourceDelete, ResourceRestore, ResourcePrune,
	)
	defaultSoftDeletedSingletonResourceVerbs = utils.NewFlags[ResourceVerb](
		ResourceGet, ResourceGetDeleted,
		ResourceCreate, ResourceUpdate, ResourceDelete, ResourceRestore, ResourcePrune,
	)
	defaultPageSize int64 = 10
)

const contentTypeApplicationJSON = "application/json"

const (
	filterQueryArg = "filter"
	sortQueryArg   = "sort"
	pageQueryArg   = "page"
)

/**
  The goal of services is that they must be easy to compose and to install
  in any supported web framework. As of today, Echo is the one we will support
  but some day we might add more frameworks.

  The idea is that services (in particular: ResourceService[IDT, RT]) are
  abstractions and shortcuts to create quick CRUD applications which MAY
  have varying degrees of permissions and configuration.

  For example, one may define the following resources:

  - Store
  - Catalog
  - Product

  In a way that all of them are available, serving available stores, per-store
  catalogs, and per-catalog products.

  Then, a design on the services:

  - StoreService (prefix: "stores", storage: StoresStorage, url arg: "s_id")
  - CatalogService (prefix: "catalogs", storage: CatalogsStorage, url arg: "c_id")
  - ProductService (prefix: "products", storage: ProductsStorage, url arg: "p_id")

  Then, the CatalogService depends on the StoreService like this:

      catalogService.AttachTo(storeService, "store_id")

  considering the Catalog resource has a field that, in JSON naming, is named "store_id":

      type Catalog struct {
          ...
          StoreId uint `json:"store_id" gorm:"..."`
          ...
      }

  The relationship to the products is similar:

      productService.AttachTo(catalogService, "catalog_id")

  Each one will have its own configuration, and its middlewares:

  - SetupMiddleware: mandatory.
  - More middlewares: optional.
  - ElementMiddleware (non-deleted elements): mandatory.

  Then, when they're installed in a framework, their paths are:

  Group /stores
      GET                   >> List stores
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourceList, "") + others
      POST                  >> Create a store
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourceCreate, "") + others
      GET /deleted          >> List deleted stores
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourceListDeleted, "") + others
      GET /{s_id}           >> Get an element (notice how "deleted" may take precedence, which
                               makes one consider that using arbitrary strings as keys might not
                               be a very good idea).
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourceGet, "") +
                    others +
                    ElementMiddleware[IDT, RT](deleted=false)
      PATCH  /{s_id}        >> Patch an element.
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourcePatch, "") +
                    others +
                    ElementMiddleware[IDT, RT](deleted=false)
      DELETE /{s_id}        >> Delete an element.
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourceDelete, "") +
                    others +
                    ElementMiddleware[IDT, RT](deleted=false)
      GET /deleted/{s_id}    >> Get a deleted element.
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourceGetDeleted, "") +
                    others +
                    ElementMiddleware[IDT, RT](deleted=true)
      POST /deleted/{s_id}   >> Restore a deleted element.
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourceRestore, "") +
                    others +
                    ElementMiddleware[IDT, RT](deleted=true)
      DELETE /deleted/{s_id} >> Prune a deleted element.
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourcePrune, "") +
                    others +
                    ElementMiddleware[IDT, RT](deleted=true)

  Considering the following:
  1. This is for a collection, not for a singleton. The example for singleton is
     something I'll add later.
  2. This assumes all the verbs are included. If the Verbs() does not include one
     specific verb, its endpoint will NOT be registered.
  3. If the underlying resource is not a SoftDeletedResource[IDT], then the endpoints
     for ResourceListDeleted, ResourceGetDeleted, ResourceRestore or ResourceGet will
     never be registered, even if the corresponding verb is specified.

  If the stores were a singleton itself, same rules would apply but the endpoints
  would be these:

  Group /stores
      POST            >> Create THE store, if not already created (deleted or not)
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourceCreate, "") + others
      GET             >> Get THE element (notice how "deleted" may take precedence, which
                         makes one consider that using arbitrary strings as keys might not
                         be a very good idea).
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourceGet, "") +
                    others +
                    ElementMiddleware[IDT, RT](deleted=false)
      PATCH           >> Patch THE element.
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourcePatch, "") +
                    others +
                    ElementMiddleware[IDT, RT](deleted=false)
      DELETE          >> Delete THE element.
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourceDelete, "") +
                    others +
                    ElementMiddleware[IDT, RT](deleted=false)
      GET /deleted    >> Get THE being-deleted element.
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourceGetDeleted, "") +
                    others +
                    ElementMiddleware[IDT, RT](deleted=true)
      POST /deleted   >> Restore THE being-deleted element.
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourceRestore, "") +
                    others +
                    ElementMiddleware[IDT, RT](deleted=true)
      DELETE /deleted >> Prune THE being-deleted element.
      - Middleware: SetupMiddleware(storeService, EndpointVerb, ResourcePrune, "") +
                    others +
                    ElementMiddleware[IDT, RT](deleted=true)

  Now, if a resource has its ResourceGet verb enabled, it can have children.
  This is done by calling:

    catalogService.MustAttachTo(storeService, "store_id")

  When doing this, it will happen that the base route of catalogService will not be
  the /catalogs one but, instead: /stores/{s_id}/cattalogs. Like this:

  Group /stores/{s_id}/catalogs
      POST ...
      ...the same stuff defined above...

  Also, the middlewares for catalog will not be just the typical SetupMiddleware +
  others + ElementMiddleware: they will have, prepended, the triplet of middlewares
  from the `GET /stores/{s_id}` endpoint above. This is solved by major frameworks
  (e.g. echo lets you define groups where each group inherits middlewares from the
  parent/ancestor groups).

  Following the idea, when the products service is attached to the catalogs, like:

    productService.MustAttachTo(catalogService, "catalog_id")

  the base route will be:

  Group /stores/{s_id}/catalogs/{c_id}/products
      POST ...
      ...the same stuff defined above...

  and the middlewares will be the (3 middlewares from stores' GET) + (3 middlewares
  from the catalogs' GET) + whatever middlewares the verb endpoints need here.
*/

// A Service is an instance that can be registered in a web application.
type Service interface {
	// Prefix stands for the prefix to use.
	Prefix() string

	// URLArg stands for the name of the url arg to use.
	// Only applies for collection services.
	URLArg() string

	// IsSingleton tells whether the resource is a singleton.
	IsSingleton() bool

	// Verbs tells the list of supported verbs.
	Verbs() utils.Flags[ResourceVerb]

	// CanHaveChildren tells whether this service can have children.
	// It is implemented as: Verbs() including ResourceGet.
	CanHaveChildren() bool

	// IsSoftDeleted tells whether the underlying resource supports
	// soft-deletion endpoints.
	IsSoftDeleted() bool

	// Children tells the services that are children of this service.
	Children() []Service

	// CollectionExtras tells the custom collection-wide endpoints
	// registered for this service.
	CollectionExtras() []ExtraEndpoint

	// ElementExtras tells the custom element-wide endpoints registered
	// for this service.
	ElementExtras() []ExtraEndpoint

	// Parent tells the parent of the current service.
	Parent() Service

	// Middlewares tell the middlewares that apply to the main group
	// or prefix for all the defined routes. Still, things apply here:
	// 1. The SetupMiddleware will NOT be included here. It will be
	//    added on its own and BEFORE all the middlewares here. This
	//    means that the SetupMiddleware will come first, and then
	//    all these middlewares. This is done per-element, since the
	//    verb is specified.
	// 2. The ElementMiddleware will not be included here. Typically,
	//    they will be included in the endpoints stated for /{prefix}/{id}
	//    and /{prefix}/deleted/{id}, right after the SetupMiddleware
	//    and all the other middlewares.
	Middlewares() []MiddlewareFunc

	// ElementMiddleware returns the typed element middleware for this service.
	ElementMiddleware(deleted bool) MiddlewareFunc

	// List defines an endpoint. Used only for COLLECTION resources and
	// installed, in its level, in: .../{prefix} -> List(context, false)
	// for live elements, or .../{prefix}/deleted -> List(context, true).
	// In both cases, using GET method. The latter case is not created if
	// RT is not SoftDeletedResource.
	List(Context, bool) error

	// Create defines an endpoint. The endpoint is POST .../{prefix}.
	Create(Context) error

	// Get defines an endpoint. The endpoint is GET .../{prefix}/{id} or
	// /{prefix}/deleted/{id}. In both cases, when registered, it will
	// belong to a group where the ElementMiddleware(deleted) will be
	// used (first case with false; second case with true).
	//
	// It is just GET .../{prefix} and .../{prefix}/deleted for singleton
	// resources.
	//
	// The /deleted case is not created if RT is not SoftDeletedResource.
	Get(Context) error

	// Update defines an endpoint. The endpoint is PATCH .../{prefix}/{id}.
	// It will belong to a group with ElementMiddleware(false) will be used.
	//
	// It is just PATCH .../{prefix} for singleton resources.
	Update(Context) error

	// Delete defines an endpoint. The endpoint is DELETE .../{prefix}/{id}.
	// It will belong to a group with ElementMiddleware(false) will be used.
	//
	// It is just DELETE .../{prefix} for singleton resources.
	Delete(Context) error

	// Prune defines an endpoint. Used when RT is a SoftDeletedResource, and
	// ignored otherwise. The endpoint is DELETE .../{prefix}/deleted/{id}.
	// It will belong to a group where ElementMiddleware(true) will be used.
	//
	// It is just DELETE .../{prefix}/deleted for singleton resources.
	//
	// This endpoint is not created if RT is not SoftDeletedResource.
	Prune(Context) error

	// Restore defines an endpoint. Used when RT is a SoftDeletedResource,
	// and ignored otherwise. The endpoint is POST .../{prefix}/deleted/{id}.
	// It will belong to a group where ElementMiddleware(true) will be
	// used.
	//
	// It is just POST .../{prefix}/deleted for singleton resources.
	//
	// This endpoint is not created if RT is not SoftDeletedResource.
	Restore(Context) error
}

// ResourceService describes a service that relates to elements
// being served through a set of known endpoints.
type ResourceService[IDT comparable, RT types.Resource[IDT]] struct {
	// The prefix is the name to use for the URL chunk for the
	// resource service in particular. Something like /foo/:id
	// or /bar will have a prefix like "foo" or "bar".
	prefix string

	// The storage field keeps the internal engine used to store
	// and retrieve elements.
	storage types.Storage[IDT, RT]

	// The singleton field tells whether this resource is singleton
	// or is it a collection.
	singleton bool

	// The urlArg is only used in collection resources to tell
	// the name of the capture parameter in the path for the
	// current resource.
	urlArg string

	// The constraintJSONField is used when the resource is child of
	// another resource: it's the JSON name of a field to look up,
	// as part of the current filter lookup.
	constraintJSONField string

	// The parentService is nil or an instance of a service.
	parentService Service

	// The childrenServices is a slice of registered children.
	childrenServices []Service

	// The collectionExtras field keeps custom routes installed
	// directly below the resource collection path.
	collectionExtras []ExtraEndpoint

	// The elementExtras field keeps custom routes installed below
	// the live element path.
	elementExtras []ExtraEndpoint

	// The verbs field tells which verbs will be considered for
	// the resource.
	verbs utils.Flags[ResourceVerb]

	// The filter field keeps a custom filter applier. By default,
	// no extra filter is applied.
	filter FilterFunc

	// The reader field keeps a custom reader for the body. By
	// default, a standard JSON-read is applied. A custom func is
	// used for when the type to read is different.
	reader ReaderFunc[IDT, RT]

	// The pageSize field tells how many elements will be rendered
	// when listing elements. By default, 10 element will be used.
	pageSize int64

	// The defaultSort function tells which one is the default sort
	// criterion for the data.
	defaultSort DefaultSortFunc

	// The allowedFields function tells the function that determines
	// the per-user allowed fields. If not set, all the fields will
	// be allowed.
	allowedFields AllowedFieldsFunc

	// The elementRenderer function tells how to render an element.
	elementRenderer ElementRendererFunc[IDT, RT]

	// The pageRenderer function tells how to render a page of elements.
	pageRenderer PageRendererFunc[IDT, RT]

	// The validator function tells what's the criterion to perform
	// the validation of a body. By default, it uses go-validate.
	// Typically, this is not used unless RT has very complex
	// validation requirements.
	validator ValidatorFunc[IDT, RT]

	// The middlewares field tells the middlewares to use for all
	// the endpoints.
	middlewares []MiddlewareFunc
}

// Prefix returns the prefix used to register this service.
func (service ResourceService[IDT, RT]) Prefix() string {
	return service.prefix
}

// Storage returns the underlying storage for this resource.
func (service ResourceService[IDT, RT]) Storage() types.Storage[IDT, RT] {
	return service.storage
}

// IsSingleton tells whether the current resource is singleton
// or not (i.e. is a resource).
func (service ResourceService[IDT, RT]) IsSingleton() bool {
	return service.singleton
}

// IsSoftDeleted tells whether the current resource supports soft-deletion.
func (service ResourceService[IDT, RT]) IsSoftDeleted() bool {
	var resource RT
	_, ok := any(resource).(types.SoftDeletedResource[IDT])
	return ok
}

// URLArg tells the name of the argument used to capture the
// id of the current element.
func (service ResourceService[IDT, RT]) URLArg() string {
	return service.urlArg
}

// Here is where the configuration starts.

// UsingVerbs sets the verbs to enable for this resource. If
// the service already has children, the ResourceGet is something
// to always force.
func (service *ResourceService[IDT, RT]) UsingVerbs(verbs ...ResourceVerb) *ResourceService[IDT, RT] {
	service.verbs = utils.NewFlags[ResourceVerb](verbs...)
	if len(service.childrenServices) > 0 || len(service.elementExtras) > 0 {
		service.verbs.Add(ResourceGet)
	}
	return service
}

func normalizeExtraEndpointMethod(method string) string {
	return strings.ToUpper(method)
}

func isValidExtraEndpointMethod(method string) bool {
	switch normalizeExtraEndpointMethod(method) {
	case "GET", "PUT", "PATCH", "POST", "DELETE":
		return true
	default:
		return false
	}
}

func sameExtraEndpoint(left ExtraEndpoint, right ExtraEndpoint) bool {
	return normalizeExtraEndpointMethod(left.Method) == normalizeExtraEndpointMethod(right.Method) &&
		left.Name == right.Name
}

func hasExtraEndpointName(endpoints []ExtraEndpoint, name string) bool {
	for _, endpoint := range endpoints {
		if endpoint.Name == name {
			return true
		}
	}
	return false
}

func validateExtraEndpoint(method string, name string, handler HandlerFunc) ExtraEndpoint {
	if err := utils.CheckPrefix(name); err != nil {
		panic(err)
	}
	if !isValidExtraEndpointMethod(method) {
		panic(ErrInvalidExtraEndpointMethod)
	}
	if handler == nil {
		panic(ErrInvalidExtraEndpointHandler)
	}
	return ExtraEndpoint{
		Method:  normalizeExtraEndpointMethod(method),
		Name:    name,
		Handler: handler,
	}
}

// MustAddCollectionExtra registers a collection-wide custom endpoint.
func (service *ResourceService[IDT, RT]) MustAddCollectionExtra(
	method string,
	name string,
	handler HandlerFunc,
) *ResourceService[IDT, RT] {
	if service.singleton {
		panic(ErrCollectionExtraOnSingleton)
	}

	endpoint := validateExtraEndpoint(method, name, handler)
	for _, registered := range service.collectionExtras {
		if sameExtraEndpoint(registered, endpoint) {
			panic(ErrConflictingExtraEndpoint)
		}
	}

	service.collectionExtras = append(service.collectionExtras, endpoint)
	return service
}

// AddCollectionExtra registers a collection-wide custom endpoint.
func (service *ResourceService[IDT, RT]) AddCollectionExtra(
	method string,
	name string,
	handler HandlerFunc,
) (err error) {
	defer func() {
		if v := recover(); v != nil {
			if err2, ok := v.(error); ok {
				err = err2
			} else {
				panic(v)
			}
		}
	}()

	service.MustAddCollectionExtra(method, name, handler)
	return nil
}

// MustAddElementExtra registers an element-wide custom endpoint.
func (service *ResourceService[IDT, RT]) MustAddElementExtra(
	method string,
	name string,
	handler HandlerFunc,
) *ResourceService[IDT, RT] {
	endpoint := validateExtraEndpoint(method, name, handler)
	for _, registered := range service.elementExtras {
		if sameExtraEndpoint(registered, endpoint) {
			panic(ErrConflictingExtraEndpoint)
		}
	}
	for _, child := range service.childrenServices {
		if child.Prefix() == name {
			panic(ErrConflictingExtraEndpoint)
		}
	}

	service.elementExtras = append(service.elementExtras, endpoint)
	if service.verbs != utils.Flags[ResourceVerb](0) {
		service.verbs.Add(ResourceGet)
	}
	return service
}

// AddElementExtra registers an element-wide custom endpoint.
func (service *ResourceService[IDT, RT]) AddElementExtra(
	method string,
	name string,
	handler HandlerFunc,
) (err error) {
	defer func() {
		if v := recover(); v != nil {
			if err2, ok := v.(error); ok {
				err = err2
			} else {
				panic(v)
			}
		}
	}()

	service.MustAddElementExtra(method, name, handler)
	return nil
}

// Verbs returns the flag of verbs to use. Children classes
// MUST override this behavior if the verbs set here are none,
// so they use the default (full) set for the resource.
func (service ResourceService[IDT, RT]) Verbs() utils.Flags[ResourceVerb] {
	if service.verbs == utils.Flags[ResourceVerb](0) {
		var r RT
		if _, ok := any(r).(types.SoftDeletedResource[IDT]); ok {
			if service.singleton {
				return defaultSoftDeletedSingletonResourceVerbs
			}
			return defaultSoftDeletedCollectionResourceVerbs
		}
		if service.singleton {
			return defaultSingletonResourceVerbs
		}
		return defaultCollectionResourceVerbs
	}
	return service.verbs
}

// UsingFilter sets the filter to use for data retrieval.
func (service *ResourceService[IDT, RT]) UsingFilter(filter FilterFunc) *ResourceService[IDT, RT] {
	service.filter = filter
	return service
}

// Filter returns the filter to use for the data retrieval.
func (service ResourceService[IDT, RT]) Filter() FilterFunc {
	return service.filter
}

// UsingReader sets the reader to use for body retrieval.
func (service *ResourceService[IDT, RT]) UsingReader(reader ReaderFunc[IDT, RT]) *ResourceService[IDT, RT] {
	service.reader = reader
	return service
}

// Reader returns the underlying reader for body data.
func (service ResourceService[IDT, RT]) Reader() ReaderFunc[IDT, RT] {
	return service.reader
}

// The read method is private and reads an element from the body, according
// to the set reader function.
func (service ResourceService[IDT, RT]) read(context Context, obj *RT) error {
	contentTypeHeader, err := context.GetHeader("Content-Type")
	if err != nil {
		return types.BadRequestError{}
	}

	contentType, _, err := mime.ParseMediaType(contentTypeHeader)
	if err != nil || contentType != contentTypeApplicationJSON {
		return types.BadRequestError{}
	}

	if service.reader != nil {
		return service.reader(context, obj)
	}
	return context.BindJSON(obj)
}

// UsingDefaultSort sets what's the sort criterion when no
// sort is specified.
func (service *ResourceService[IDT, RT]) UsingDefaultSort(defaultSort DefaultSortFunc) *ResourceService[IDT, RT] {
	if service.singleton && defaultSort != nil {
		logger.Warn(
			"default sort is not used in singleton resources",
			"prefix", service.prefix,
		)
	} else {
		service.defaultSort = defaultSort
	}

	return service
}

// DefaultSort returns the default sort function (the function
// that tells which sort to apply when it's not specified).
func (service ResourceService[IDT, RT]) DefaultSort() DefaultSortFunc {
	return service.defaultSort
}

// UsingAllowedFields sets what's the criterion to select the
// allowed fields for a query. By default, all the valid fields
// are allowed (for filter and sort).
func (service *ResourceService[IDT, RT]) UsingAllowedFields(allowedFields AllowedFieldsFunc) *ResourceService[IDT, RT] {
	service.allowedFields = allowedFields
	return service
}

// AllowedFields returns the function that tells what are the
// per-user allowed fields.
func (service ResourceService[IDT, RT]) AllowedFields() AllowedFieldsFunc {
	return service.allowedFields
}

// UsingValidator sets what's the validator to use for RT.
func (service *ResourceService[IDT, RT]) UsingValidator(validator ValidatorFunc[IDT, RT]) *ResourceService[IDT, RT] {
	service.validator = validator
	return service
}

// Validator returns the validator being used.
func (service ResourceService[IDT, RT]) Validator() ValidatorFunc[IDT, RT] {
	return service.validator
}

// UsingMiddlewares tells the middlewares will be used.
// Middlewares are evaluated left-to-right (they also end
// right-to-left, since they're like onion layers).
func (service *ResourceService[IDT, RT]) UsingMiddlewares(middlewares ...MiddlewareFunc) *ResourceService[IDT, RT] {
	service.middlewares = middlewares
	return service
}

// Middlewares returns the list of middlewares used.
func (service ResourceService[IDT, RT]) Middlewares() []MiddlewareFunc {
	if service.middlewares == nil {
		return nil
	}
	middlewares := make([]MiddlewareFunc, len(service.middlewares))
	copy(middlewares, service.middlewares)
	return middlewares
}

// UsingElementRenderer sets what's the element renderer.
func (service *ResourceService[IDT, RT]) UsingElementRenderer(elementRenderer ElementRendererFunc[IDT, RT]) *ResourceService[IDT, RT] {
	service.elementRenderer = elementRenderer
	return service
}

// ElementRenderer returns the element renderer being used.
func (service ResourceService[IDT, RT]) ElementRenderer() ElementRendererFunc[IDT, RT] {
	return service.elementRenderer
}

// UsingPageRenderer sets what's the page renderer.
func (service *ResourceService[IDT, RT]) UsingPageRenderer(pageRenderer PageRendererFunc[IDT, RT]) *ResourceService[IDT, RT] {
	service.pageRenderer = pageRenderer
	return service
}

// PageRenderer returns the element renderer being used.
func (service ResourceService[IDT, RT]) PageRenderer() PageRendererFunc[IDT, RT] {
	return service.pageRenderer
}

// RenderElement renders a single element, perhaps using the renderer.
func (service ResourceService[IDT, RT]) RenderElement(context Context, status int, element RT) error {
	if service.elementRenderer != nil {
		return service.elementRenderer(context, element)
	}
	return context.RenderJSON(status, element)
}

// RenderPage renders a page of elements, perhaps using the renderer.
func (service ResourceService[IDT, RT]) RenderPage(context Context, status int, elements []RT, page int64, totalPages int64) error {
	if service.pageRenderer != nil {
		return service.pageRenderer(context, elements, page, totalPages)
	}
	return context.RenderJSON(status, map[string]any{
		"elements":   elements,
		"page":       page,
		"totalPages": totalPages,
	})
}

// UsingPageSize sets the amount of elements being listed per page.
func (service *ResourceService[IDT, RT]) UsingPageSize(pageSize int64) *ResourceService[IDT, RT] {
	if pageSize <= 0 {
		logger.Warn(fmt.Sprintf("invalid page size - changing to %d", defaultPageSize))
		pageSize = defaultPageSize
	}

	if service.singleton {
		logger.Warn(
			"page size is not used in singleton resources",
			"prefix", service.prefix,
		)
	}

	service.pageSize = pageSize
	return service
}

// PageSize returns the amount of elements being listed per page.
func (service ResourceService[IDT, RT]) PageSize() int64 {
	if service.pageSize == 0 {
		return defaultPageSize
	}
	return service.pageSize
}

// CanHaveChildren tells whether a service can register children
// (by other services registering as children of it).
func (service ResourceService[IDT, RT]) CanHaveChildren() bool {
	return service.Verbs().Has(ResourceGet)
}

// ElementMiddleware returns the typed element middleware for this service.
func (service *ResourceService[IDT, RT]) ElementMiddleware(deleted bool) MiddlewareFunc {
	return ElementMiddleware[IDT, RT](deleted)
}

type childAppender interface {
	addChild(Service)
}

type mappingProvider interface {
	resourceMapping() *types.FieldsMapping
}

func sameService(left Service, right Service) bool {
	if left == nil || right == nil {
		return false
	}

	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	if leftValue.Kind() == reflect.Pointer && rightValue.Kind() == reflect.Pointer {
		return leftValue.Type() == rightValue.Type() && leftValue.Pointer() == rightValue.Pointer()
	}

	if leftValue.Type().Comparable() && rightValue.Type().Comparable() {
		return left == right
	}

	return false
}

func (service *ResourceService[IDT, RT]) addChild(child Service) {
	for _, registered := range service.childrenServices {
		if sameService(registered, child) {
			return
		}
	}
	if hasExtraEndpointName(service.elementExtras, child.Prefix()) {
		panic(ErrConflictingExtraEndpoint)
	}

	service.childrenServices = append(service.childrenServices, child)
}

func (service *ResourceService[IDT, RT]) resourceMapping() *types.FieldsMapping {
	if service.storage == nil {
		return nil
	}
	return service.storage.Mapping()
}

// MustAttachTo attaches the current service to another service.
// It fails, panicking, under the following conditions:
//   - s being null.
//   - current service already attached to another service.
//   - s being the current service.
//   - Traversing the .Parent() upward in `s`, it is found that
//     the current service is in the path (causing a cycle), or
//     the current service is a Collection and also the URL Arg
//     of the current service is found while traversing.
//   - The current service prefix conflicts with an element extra
//     endpoint already registered in the parent service.
func (service *ResourceService[IDT, RT]) MustAttachTo(s Service, constraintJSONField string) {
	if s == nil || !s.CanHaveChildren() {
		panic(ErrInvalidParentService)
	}
	if service.parentService != nil {
		panic(ErrAlreadyAttached)
	}

	for parent := s; parent != nil; parent = parent.Parent() {
		if sameService(parent, service) {
			panic(ErrCyclicServiceAttachment)
		}

		if !service.singleton && !parent.IsSingleton() && parent.URLArg() == service.urlArg {
			panic(ErrConflictingServiceURLArg)
		}
	}

	if !s.IsSingleton() {
		if service.storage == nil {
			panic(ErrInvalidConstraintJSONField)
		}

		childMapping := service.storage.Mapping()
		childField, ok := types.StructFieldForJSON(childMapping, constraintJSONField)
		if !ok {
			panic(ErrInvalidConstraintJSONField)
		}

		provider, ok := s.(mappingProvider)
		if !ok || provider.resourceMapping() == nil {
			panic(ErrInvalidParentService)
		}
		parentIDType := provider.resourceMapping().IDType()
		if !isAssignableOrConvertible(parentIDType, childField.Type) {
			panic(ErrInvalidConstraintIDType)
		}

		// This endpoint is not created if RT is not SoftDeletedResource.

		service.constraintJSONField = constraintJSONField
	}
	if hasExtraEndpointName(s.ElementExtras(), service.Prefix()) {
		panic(ErrConflictingExtraEndpoint)
	}
	service.parentService = s
	if appender, ok := s.(childAppender); ok {
		appender.addChild(service)
	}
}

// AttachTo attaches the current service to another service. It
// fails on the same conditions MustAttach fails, but returns
// an error instead of panicking.
func (service *ResourceService[IDT, RT]) AttachTo(s Service, constraintJSONField string) (err error) {
	defer func() {
		if v := recover(); v != nil {
			if err2, ok := v.(error); ok {
				err = err2
			} else {
				panic(v)
			}
		}
	}()

	service.MustAttachTo(s, constraintJSONField)
	return nil
}

// Children returns the registered children of the service.
func (service ResourceService[IDT, RT]) Children() []Service {
	if service.childrenServices == nil {
		return nil
	}
	copy_ := make([]Service, len(service.childrenServices))
	copy(copy_, service.childrenServices)
	return copy_
}

// CollectionExtras returns the collection-wide custom endpoints.
func (service ResourceService[IDT, RT]) CollectionExtras() []ExtraEndpoint {
	if service.collectionExtras == nil {
		return nil
	}
	extras := make([]ExtraEndpoint, len(service.collectionExtras))
	copy(extras, service.collectionExtras)
	return extras
}

// ElementExtras returns the element-wide custom endpoints.
func (service ResourceService[IDT, RT]) ElementExtras() []ExtraEndpoint {
	if service.elementExtras == nil {
		return nil
	}
	extras := make([]ExtraEndpoint, len(service.elementExtras))
	copy(extras, service.elementExtras)
	return extras
}

// Parent returns the registered parent of this service.
func (service ResourceService[IDT, RT]) Parent() Service {
	return service.parentService
}

// Here is where the utility functions for the middleware start.

// getStackedElement gets the element at the last constraint level.
func (service ResourceService[IDT, RT]) getStackedElement(context Context, index int) (RT, error) {
	var zero RT
	lastRaw, exists := context.PeekElement(index)
	if !exists {
		logger.Error("no element in context stack - provably called outside element middleware")
		return zero, types.InternalError{}
	}

	last, ok := lastRaw.(RT)
	if !ok {
		logger.Error("invalid element in context stack - provably called outside element middleware")
		return zero, types.InternalError{}
	}

	return last, nil
}

func getStackedElementID(context Context, index int) (any, error) {
	lastRaw, exists := context.PeekElement(index)
	if !exists {
		logger.Error("no element in context stack - provably called outside element middleware")
		return nil, types.InternalError{}
	}

	value := reflect.ValueOf(lastRaw)
	method := value.MethodByName("GetID")
	if !method.IsValid() || method.Type().NumIn() != 0 || method.Type().NumOut() != 1 {
		logger.Error("invalid element in context stack - element does not expose GetID")
		return nil, types.InternalError{}
	}

	return method.Call(nil)[0].Interface(), nil
}

// makeElementFilter assembles a filter from the current request.
func (service ResourceService[IDT, RT]) makeElementFilter(context Context, deleted bool) (
	*types.FilterExpression, IDT, error,
) {
	// First, declare the filter.
	var filter types.FilterExpression
	var id IDT

	if !service.singleton {
		// 1. Get the ID from the path.
		urlArg := service.urlArg
		rawId, err := context.GetPathParam(urlArg)
		if err != nil {
			return nil, id, types.NotFoundError[string]{}
		}

		// 2. Second, parse it to a valid value.
		id, err := ParsePathParam[IDT](rawId)
		if err != nil {
			return nil, id, types.InvalidIDError{
				ElementName: service.prefix,
				Key:         rawId,
			}
		}

		// 3. Add the ID filter.
		service.storage.AddIDFilter(&filter, id)
	}

	// Then, add the per-context filter.
	if service.filter != nil {
		service.filter(context, &filter)
	}

	// Then, add a constraint, if any.
	if service.constraintJSONField != "" {
		// We use index 0 since the idea is to get the constraint
		// based on the current (last) element.
		parentID, err := getStackedElementID(context, 0)
		if err != nil {
			return nil, id, err
		}

		filter.Restrict(&types.FilterExpression{
			Operator:    types.FilterEQ,
			Field:       service.constraintJSONField,
			Value:       parentID,
			Expressions: nil,
		})
	}

	// Then, add the per-deleted filter.
	service.storage.AddDeletedFilter(&filter, deleted)

	// Finally, validate the filter.
	if err := service.storage.ValidateFilter(&filter); err != nil {
		logger.Error("invalid filter (should be fixed, since the user is not choosing this one)")
		return nil, id, types.InternalError{}
	}

	// And return.
	return &filter, id, nil
}

// applyPreviousConstraint applies the current constraint to the element, so it's
// always consistent.
func (service ResourceService[IDT, RT]) applyPreviousConstraint(context Context, element *RT) error {
	return service.applyConstraintFromStack(context, element, 1)
}

func (service ResourceService[IDT, RT]) applyCurrentConstraint(context Context, element *RT) error {
	return service.applyConstraintFromStack(context, element, 0)
}

func (service ResourceService[IDT, RT]) applyConstraintFromStack(context Context, element *RT, index int) error {
	if service.constraintJSONField != "" {
		parentID, err := getStackedElementID(context, index)
		if err != nil {
			return err
		}

		fieldName := types.FieldForJSON(service.storage.Mapping(), service.constraintJSONField)
		if fieldName == "" {
			logger.Error(
				"constraint field is not mapped in resource",
				"prefix", service.prefix,
				"field", service.constraintJSONField,
			)
			return types.InternalError{}
		}

		if err := setElementField(element, fieldName, parentID); err != nil {
			logger.Error(
				"could not apply constraint to resource element",
				"prefix", service.prefix,
				"field", service.constraintJSONField,
				"struct_field", fieldName,
				"error", err,
			)
			return types.InternalError{}
		}
	}

	return nil
}

func isAssignableOrConvertible(source reflect.Type, target reflect.Type) bool {
	if source == nil || target == nil {
		return false
	}
	if source.AssignableTo(target) {
		return true
	}
	if source.Kind() == target.Kind() && source.ConvertibleTo(target) {
		return true
	}
	return false
}

func setElementField(element any, fieldName string, value any) error {
	if element == nil {
		return errors.New("element is nil")
	}

	elementValue := reflect.ValueOf(element)
	if elementValue.Kind() != reflect.Pointer || elementValue.IsNil() {
		return errors.New("element must be a non-nil pointer")
	}

	valueValue := reflect.ValueOf(value)
	if !valueValue.IsValid() {
		return errors.New("constraint value is invalid")
	}

	for elementValue.Kind() == reflect.Pointer {
		if elementValue.IsNil() {
			return errors.New("element contains nil pointer")
		}
		elementValue = elementValue.Elem()
	}

	if elementValue.Kind() != reflect.Struct {
		return errors.New("element must point to a struct")
	}

	fieldValue := elementValue.FieldByName(fieldName)
	if !fieldValue.IsValid() {
		return fmt.Errorf("field %q not found", fieldName)
	}
	if !fieldValue.CanSet() {
		return fmt.Errorf("field %q cannot be set", fieldName)
	}

	if valueValue.Type().AssignableTo(fieldValue.Type()) {
		fieldValue.Set(valueValue)
		return nil
	}
	if valueValue.Kind() == fieldValue.Kind() && valueValue.Type().ConvertibleTo(fieldValue.Type()) {
		fieldValue.Set(valueValue.Convert(fieldValue.Type()))
		return nil
	}

	return fmt.Errorf("value of type %s cannot be assigned to field %q of type %s", valueValue.Type(), fieldName, fieldValue.Type())
}

func (service ResourceService[IDT, RT]) validate(context Context, element RT) error {
	if service.validator != nil {
		return service.validator(context, element)
	}
	return utils.Validate(element)
}

func (service ResourceService[IDT, RT]) renderNotFound(context Context, id IDT) error {
	if service.singleton {
		return renderError(context, types.SingletonNotFoundError{
			ElementName: service.prefix,
		})
	}

	return renderError(context, types.NotFoundError[IDT]{
		ElementName: service.prefix,
		Key:         id,
	})
}

func (service ResourceService[IDT, RT]) renderStorageError(context Context, err error) error {
	return renderErrorOr(context, err, types.InternalError{})
}

func (service ResourceService[IDT, RT]) ensureSingletonCreateAllowed(context Context) (bool, error) {
	if !service.singleton {
		return true, nil
	}

	activeFilter, _, err := service.makeElementFilter(context, false)
	if err != nil {
		return false, renderErrorOr(context, err, types.BadRequestError{})
	}

	if _, found, err := service.storage.GetElement(activeFilter); err != nil {
		return false, service.renderStorageError(context, err)
	} else if found {
		return false, renderError(context, types.SingletonAlreadyExistsError{})
	}

	var zero RT
	if _, ok := any(zero).(types.SoftDeletedResource[IDT]); !ok {
		return true, nil
	}

	deletedFilter, _, err := service.makeElementFilter(context, true)
	if err != nil {
		return false, renderErrorOr(context, err, types.BadRequestError{})
	}

	if _, found, err := service.storage.GetElement(deletedFilter); err != nil {
		return false, service.renderStorageError(context, err)
	} else if found {
		return false, renderError(context, types.SingletonDeletedExistsError{})
	}

	return true, nil
}

func (service ResourceService[IDT, RT]) allowedFieldsFor(context Context) ([]string, Allowance) {
	if service.allowedFields == nil {
		return nil, All
	}
	return service.allowedFields(context)
}

func (service ResourceService[IDT, RT]) parseListFilter(context Context, fields []string, allowance Allowance) (*types.FilterExpression, error) {
	rawFilter, err := context.GetQueryParam(filterQueryArg)
	if err != nil {
		return &types.FilterExpression{}, nil
	}

	rawFilter = strings.TrimSpace(rawFilter)
	if rawFilter == "" {
		return &types.FilterExpression{}, nil
	}

	filter, err := types.NewFilterParser(allowedListValidator{
		fields:    fields,
		allowance: allowance,
	}).Parse(json.NewDecoder(bytes.NewBufferString(rawFilter)))
	if err != nil {
		return nil, err
	}

	return &filter, nil
}

func (service ResourceService[IDT, RT]) parseListSort(context Context, fields []string, allowance Allowance) (*types.SortExpression, error) {
	rawSort, err := context.GetQueryParam(sortQueryArg)
	if err != nil {
		rawSort = ""
	}

	rawSort = strings.TrimSpace(rawSort)
	if rawSort != "" {
		sort, err := types.NewSortParser(allowedListValidator{
			fields:    fields,
			allowance: allowance,
		}).Parse(rawSort)
		if err != nil {
			return nil, err
		}

		return &sort, nil
	}

	if service.defaultSort == nil {
		return &types.SortExpression{}, nil
	}

	sort := service.defaultSort(context)
	return &sort, nil
}

func parseListPage(context Context) (int64, error) {
	rawPage, err := context.GetQueryParam(pageQueryArg)
	if err != nil {
		return 0, nil
	}

	rawPage = strings.TrimSpace(rawPage)
	if rawPage == "" {
		return 0, nil
	}

	page, err := strconv.ParseInt(rawPage, 10, 64)
	if err != nil {
		return 0, err
	}
	if page < 0 {
		return 0, nil
	}

	return page, nil
}

func (service ResourceService[IDT, RT]) applyListRestrictions(context Context, filter *types.FilterExpression, deleted bool) error {
	if service.filter != nil {
		service.filter(context, filter)
	}

	if service.constraintJSONField != "" {
		parentID, err := getStackedElementID(context, 0)
		if err != nil {
			return err
		}

		filter.Restrict(&types.FilterExpression{
			Operator:    types.FilterEQ,
			Field:       service.constraintJSONField,
			Value:       parentID,
			Expressions: nil,
		})
	}

	service.storage.AddDeletedFilter(filter, deleted)
	return service.storage.ValidateFilter(filter)
}

func listTotalPages(total int64, pageSize int64) int64 {
	if total <= 0 {
		return 0
	}
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}

	return (total + pageSize - 1) / pageSize
}

type allowedListValidator struct {
	fields    []string
	allowance Allowance
}

func (v allowedListValidator) IsValidCmpFilter(field string, _ any) bool {
	return v.isAllowed(field)
}

func (v allowedListValidator) IsNullCheckable(field string) bool {
	return v.isAllowed(field)
}

func (v allowedListValidator) IsExistenceCheckable(field string) bool {
	return v.isAllowed(field)
}

func (v allowedListValidator) IsContainsCheckable(field string) bool {
	return v.isAllowed(field)
}

func (v allowedListValidator) IsSortable(field string, _ types.OrderType) bool {
	return v.isAllowed(field)
}

func (v allowedListValidator) isAllowed(field string) bool {
	switch v.allowance {
	case Only:
		return containsAllowedField(v.fields, field)
	case Except:
		return !containsAllowedField(v.fields, field)
	default:
		return true
	}
}

func containsAllowedField(fields []string, field string) bool {
	for _, allowedField := range fields {
		if allowedField == field {
			return true
		}
	}

	return false
}

// The Get function is an endpoint to get a single element.
// Pre-requisites:
// - ElementMiddleware(false) middleware for GET.
// - ElementMiddleware(true) middleware for GET DELETED.
func (service ResourceService[IDT, RT]) Get(context Context) error {
	// 1. Get the element.
	element, err := service.getStackedElement(context, 0)
	if err != nil {
		return err
	}

	// 2. Render it.
	return service.RenderElement(context, 200, element)
}

// The Update function is an endpoint to update an existing non-deleted element.
// Pre-requisites: ElementMiddleware(false) middleware.
func (service ResourceService[IDT, RT]) Update(context Context) error {
	element, err := service.getStackedElement(context, 0)
	if err != nil {
		return err
	}

	id := element.GetID()
	createdAt := element.GetCreationTime()

	if err := service.read(context, &element); err != nil {
		return renderErrorOr(context, err, types.BadRequestError{})
	}

	element.SetID(id)
	element.RestoreCreationTime(createdAt)

	if err := service.applyPreviousConstraint(context, &element); err != nil {
		return renderErrorOr(context, err, types.InternalError{})
	}

	if err := service.validate(context, element); err != nil {
		return renderErrorOr(context, err, types.ValidationError{})
	}

	if notFound, err := service.storage.Save(&element); err != nil {
		return service.renderStorageError(context, err)
	} else if notFound {
		return service.renderNotFound(context, id)
	}

	return service.RenderElement(context, 200, element)
}

// The Create function is an endpoint to create a new element.
func (service ResourceService[IDT, RT]) Create(context Context) error {
	allowed, err := service.ensureSingletonCreateAllowed(context)
	if err != nil || !allowed {
		return err
	}

	var element RT
	if err := service.read(context, &element); err != nil {
		return renderErrorOr(context, err, types.BadRequestError{})
	}

	var zero IDT
	element.SetID(zero)
	element.SetCreationTime()

	if err := service.applyCurrentConstraint(context, &element); err != nil {
		return renderErrorOr(context, err, types.InternalError{})
	}

	if err := service.validate(context, element); err != nil {
		return renderErrorOr(context, err, types.ValidationError{})
	}

	if notFound, err := service.storage.Save(&element); err != nil {
		return service.renderStorageError(context, err)
	} else if notFound {
		return renderError(context, types.InternalError{})
	}

	return service.RenderElement(context, 201, element)
}

// The Delete function is an endpoint to delete an existing non-deleted element.
// Pre-requisites: ElementMiddleware(false) middleware.
func (service ResourceService[IDT, RT]) Delete(context Context) error {
	element, err := service.getStackedElement(context, 0)
	if err != nil {
		return err
	}

	if notFound, err := service.storage.Delete(&element); err != nil {
		return service.renderStorageError(context, err)
	} else if notFound {
		return service.renderNotFound(context, element.GetID())
	}

	return context.RenderNoContent(204)
}

// The Prune function is an endpoint to permanently delete an existing deleted
// element. Pre-requisites: ElementMiddleware(true) middleware.
func (service ResourceService[IDT, RT]) Prune(context Context) error {
	element, err := service.getStackedElement(context, 0)
	if err != nil {
		return err
	}

	if notFound, err := service.storage.Prune(&element); err != nil {
		return service.renderStorageError(context, err)
	} else if notFound {
		return service.renderNotFound(context, element.GetID())
	}

	return context.RenderNoContent(204)
}

// The Restore function is an endpoint to restore an existing deleted element.
// Pre-requisites: ElementMiddleware(true) middleware.
func (service ResourceService[IDT, RT]) Restore(context Context) error {
	element, err := service.getStackedElement(context, 0)
	if err != nil {
		return err
	}

	if notFound, err := service.storage.Restore(&element); err != nil {
		return service.renderStorageError(context, err)
	} else if notFound {
		return service.renderNotFound(context, element.GetID())
	}

	return service.RenderElement(context, 200, element)
}

// The List function is an endpoint to list existing non-deleted or deleted
// elements. Use deleted=false for ResourceList and deleted=true for
// ResourceListDeleted.
func (service ResourceService[IDT, RT]) List(context Context, deleted bool) error {
	fields, allowance := service.allowedFieldsFor(context)

	filter, err := service.parseListFilter(context, fields, allowance)
	if err != nil {
		return renderError(context, types.BadRequestError{})
	}

	sort, err := service.parseListSort(context, fields, allowance)
	if err != nil {
		return renderError(context, types.BadRequestError{})
	}

	if err := service.storage.ValidateSort(sort); err != nil {
		return renderError(context, types.BadRequestError{})
	}

	if err := service.applyListRestrictions(context, filter, deleted); err != nil {
		return renderErrorOr(context, err, types.BadRequestError{})
	}

	page, err := parseListPage(context)
	if err != nil {
		return renderError(context, types.BadRequestError{})
	}

	pageSize := service.PageSize()
	elements, total, err := service.storage.GetElements(filter, sort, page*pageSize, pageSize)
	if err != nil {
		return service.renderStorageError(context, err)
	}

	return service.RenderPage(context, 200, elements, page, listTotalPages(total, pageSize))
}

// MustCreateSingletonService creates a singleton service, panicking if
// the prefix is invalid or the storage is null.
func MustCreateSingletonService[IDT comparable, RT types.Resource[IDT]](
	prefix string, storage types.Storage[IDT, RT],
) *ResourceService[IDT, RT] {
	if err := utils.CheckPrefix(prefix); err != nil {
		panic(err)
	}

	if storage == nil {
		panic(ErrInvalidStorage)
	}

	return &ResourceService[IDT, RT]{
		storage:   storage,
		prefix:    prefix,
		singleton: true,
	}
}

// CreateSingletonService creates a singleton service, returning an error
// if the prefix is invalid or the storage is null.
func CreateSingletonService[IDT comparable, RT types.Resource[IDT]](
	prefix string, storage types.Storage[IDT, RT],
) (res *ResourceService[IDT, RT], err error) {
	defer func() {
		if v := recover(); v != nil {
			if err2, ok := v.(error); ok {
				res = nil
				err = err2
			} else {
				panic(v)
			}
		}
	}()

	return MustCreateSingletonService(prefix, storage), nil
}

// MustCreateCollectionService creates a singleton service, panicking if
// the prefix is invalid, the URL arg is invalid, or the storage is null.
func MustCreateCollectionService[IDT comparable, RT types.Resource[IDT]](
	prefix string, urlArg string, storage types.Storage[IDT, RT],
) *ResourceService[IDT, RT] {
	if err := utils.CheckPrefix(prefix); err != nil {
		panic(err)
	}

	if err := utils.CheckURLArg(urlArg); err != nil {
		panic(err)
	}

	if storage == nil {
		panic(ErrInvalidStorage)
	}

	return &ResourceService[IDT, RT]{
		storage:   storage,
		prefix:    prefix,
		singleton: false,
		urlArg:    urlArg,
	}
}

// CreateCollectionService creates a singleton service, returning an error
// if the prefix is invalid or the storage is null.
func CreateCollectionService[IDT comparable, RT types.Resource[IDT]](
	prefix string, urlArg string, storage types.Storage[IDT, RT],
) (res *ResourceService[IDT, RT], err error) {
	defer func() {
		if v := recover(); v != nil {
			if err2, ok := v.(error); ok {
				res = nil
				err = err2
			} else {
				panic(v)
			}
		}
	}()

	return MustCreateCollectionService(prefix, urlArg, storage), nil
}
