# Full API Documentation

This reference documents the public packages and the extension points most users
need. See also:

- [Callbacks and middleware](callbacks.md)
- [Storage adapters](storage.md)
- [Filter and sort syntax](queries.md)

## `echo`

Import path:

```go
github.com/universe-10th/rest-resources/echo
```

### Installation

`GroupProvider` is implemented by both `*echo.Echo` and `*echo.Group`:

```go
type GroupProvider interface {
	Group(prefix string, m ...echo.MiddlewareFunc) *echo.Group
}
```

`MustInstall(app GroupProvider, service services.Service)` installs a root
service and panics on invalid input.

`Install(app GroupProvider, service services.Service) error` does the same work
but returns known validation errors instead of panicking.

Install errors:

- `ErrInvalidEchoApp`: nil app or group.
- `ErrInvalidService`: nil service.
- `ErrInvalidRootService`: attempted to install a child service directly.

### Context

`WrapContext(c echo.Context) *Context` returns the framework-neutral
`services.Context` wrapper for an Echo request.

`Context` implements:

- request reads: `Native`, `GetPathParam`, `GetQueryParam`, `GetQueryParams`,
  `GetHeader`, `GetHeaders`, `GetCookie`, `BindJSON`
- response writes: `SetHeader`, `SetCookie`, `RenderJSON`, `RenderNoContent`
- request data: `GetData`, `SetData`
- resource stack: `PushElement`, `PopElement`, `PeekElement`
- endpoint metadata: `CurrentService`, `CurrentEndpoint`, `Setup`

## `types/services`

Import path:

```go
github.com/universe-10th/rest-resources/types/services
```

### Constructors

Collections:

```go
MustCreateCollectionService[IDT, RT](prefix, urlArg string, storage types.Storage[IDT, RT]) *ResourceService[IDT, RT]
CreateCollectionService[IDT, RT](prefix, urlArg string, storage types.Storage[IDT, RT]) (*ResourceService[IDT, RT], error)
```

Singletons:

```go
MustCreateSingletonService[IDT, RT](prefix string, storage types.Storage[IDT, RT]) *ResourceService[IDT, RT]
CreateSingletonService[IDT, RT](prefix string, storage types.Storage[IDT, RT]) (*ResourceService[IDT, RT], error)
```

Constructor errors:

- `ErrInvalidStorage`
- invalid prefix or URL argument from `utils.CheckPrefix` and
  `utils.CheckURLArg`

### Service Interface

`Service` is the framework-neutral contract installed by adapters. It exposes
metadata, children, middleware, and endpoint handlers:

- metadata: `Prefix`, `URLArg`, `IsSingleton`, `IsSoftDeleted`, `Verbs`,
  `CanHaveChildren`
- tree: `Children`, `Parent`
- custom endpoints: `CollectionExtras`, `ElementExtras`
- middleware: `Middlewares`, `ElementMiddleware`
- endpoints: `List`, `Create`, `Get`, `Update`, `Delete`, `Prune`, `Restore`

### `ResourceService`

`ResourceService[IDT, RT]` is the standard `Service` implementation.

Read-only accessors:

- `Prefix() string`
- `Storage() types.Storage[IDT, RT]`
- `IsSingleton() bool`
- `IsSoftDeleted() bool`
- `URLArg() string`
- `Verbs() utils.Flags[ResourceVerb]`
- `Filter() FilterFunc`
- `Reader() ReaderFunc[IDT, RT]`
- `DefaultSort() DefaultSortFunc`
- `AllowedFields() AllowedFieldsFunc`
- `Validator() ValidatorFunc[IDT, RT]`
- `Middlewares() []MiddlewareFunc`
- `ElementRenderer() ElementRendererFunc[IDT, RT]`
- `PageRenderer() PageRendererFunc[IDT, RT]`
- `PageSize() int64`
- `Children() []Service`
- `CollectionExtras() []ExtraEndpoint`
- `ElementExtras() []ExtraEndpoint`
- `Parent() Service`

Builder-style configuration:

- `UsingVerbs(...ResourceVerb)`
- `UsingFilter(FilterFunc)`
- `UsingReader(ReaderFunc[IDT, RT])`
- `UsingDefaultSort(DefaultSortFunc)`
- `UsingAllowedFields(AllowedFieldsFunc)`
- `UsingValidator(ValidatorFunc[IDT, RT])`
- `UsingMiddlewares(...MiddlewareFunc)`
- `UsingElementRenderer(ElementRendererFunc[IDT, RT])`
- `UsingPageRenderer(PageRendererFunc[IDT, RT])`
- `UsingPageSize(int64)`

Custom endpoint configuration:

- `AddCollectionExtra(method, name string, handler HandlerFunc) error`
- `MustAddCollectionExtra(method, name string, handler HandlerFunc)`
- `AddElementExtra(method, name string, handler HandlerFunc) error`
- `MustAddElementExtra(method, name string, handler HandlerFunc)`

Custom endpoints support `GET`, `PUT`, `PATCH`, `POST`, and `DELETE`. Names use
the same validation as resource prefixes. Collection extras install at
`/{prefix}/{name}` and are rejected for singleton resources. Element extras
install at `/{prefix}/{id}/{name}` for collections and `/{prefix}/{name}` for
singletons; they use live element middleware and cannot share a name with a
child resource.

Tree methods:

- `AttachTo(parent Service, constraintJSONField string) error`
- `MustAttachTo(parent Service, constraintJSONField string)`

Attachment errors:

- `ErrInvalidParentService`
- `ErrAlreadyAttached`
- `ErrCyclicServiceAttachment`
- `ErrConflictingServiceURLArg`
- `ErrInvalidConstraintJSONField`
- `ErrInvalidConstraintIDType`
- `ErrInvalidExtraEndpointMethod`
- `ErrInvalidExtraEndpointHandler`
- `ErrConflictingExtraEndpoint`
- `ErrCollectionExtraOnSingleton`

Endpoint methods are public so adapters can invoke them:

- `List(context Context, deleted bool) error`
- `Create(context Context) error`
- `Get(context Context) error`
- `Update(context Context) error`
- `Delete(context Context) error`
- `Restore(context Context) error`
- `Prune(context Context) error`

Rendering helpers:

- `RenderElement(context Context, status int, element RT) error`
- `RenderPage(context Context, status int, elements []RT, page, totalPages int64) error`

### Verbs

`ResourceVerb` controls which standard endpoints are installed:

- `ResourceGet`
- `ResourceList`
- `ResourceCreate`
- `ResourceUpdate`
- `ResourceDelete`
- `ResourceGetDeleted`
- `ResourceListDeleted`
- `ResourceRestore`
- `ResourcePrune`

If `UsingVerbs` is not called, services use defaults based on singleton vs
collection and hard delete vs soft delete.

### Callbacks

Service callbacks and middleware are documented in
[callbacks and middleware](callbacks.md).

### Context

`Context` is the callback-facing request and response API. It exposes request
metadata, JSON binding, response rendering, cookies, arbitrary request data,
endpoint metadata, and a stack of parent/current resource elements.

`ParsePathParam[T comparable](value string) (T, error)` parses strings into
supported ID types: strings, signed and unsigned integers, bools, and types that
implement `encoding.TextUnmarshaler`.

## `types`

Import path:

```go
github.com/universe-10th/rest-resources/types
```

### Resource Interfaces

`Identified[T]` requires:

- `GetID() T`
- `SetID(T)`
- `GetIDField() string`

`Timestamps` requires creation and update timestamp getters, setters, and JSON
field names.

`DeletionTimestamp` requires deletion timestamp getters, setters, clearing, and
JSON field name.

`Resource[T]` combines `Identified[T]` and `Timestamps`.

`SoftDeletedResource[T]` combines `Resource[T]` and `DeletionTimestamp`.

### Storage Interface

`Storage[IDT, RT]` is implemented by memory, GORM, MongoDB, and custom adapters:

- `Mapping() *FieldsMapping`
- `GetElement(filter *FilterExpression) (RT, bool, error)`
- `GetElements(filter *FilterExpression, sort *SortExpression, skip, limit int64) ([]RT, int64, error)`
- `Save(element *RT) (notFound bool, err error)`
- `Delete(element *RT) (notFound bool, err error)`
- `ValidateFilter(filter *FilterExpression) error`
- `ValidateSort(sort *SortExpression) error`
- `AddIDFilter(filter *FilterExpression, id IDT)`
- `Restore(element *RT) (notFound bool, err error)`
- `Prune(element *RT) (notFound bool, err error)`
- `AddDeletedFilter(filter *FilterExpression, deleted bool)`

### Filters and Sort

Core parser and DSL types:

- `FilterParser`, `NewFilterParser`
- `FilterExpression`, `FilterOperator`
- `FilterSerializer`, `FilterValidator`, `FilterSource`
- `SortParser`, `NewSortParser`
- `SortExpression`, `Sort`, `OrderType`
- `SortSerializer`, `SortValidator`, `SortSource`

See [filter and sort syntax](queries.md).

### Field Mapping

`FieldsMapping` links JSON field names to struct fields and storage fields.

Public helpers:

- `JSONToField(v any) map[string]string`
- `NewFieldsMapping[IDT, RT](customFieldToStorage MappingFunc) *FieldsMapping`
- `FieldForJSON(mapping, jsonName) string`
- `StorageForJSON(mapping, jsonName) string`
- `StructFieldForJSON(mapping, jsonName) (reflect.StructField, bool)`
- `StructFieldByName(valueType, fieldName) (reflect.StructField, bool)`
- scalar helpers: `IsIntegralNumber`, `NumericValue`, `AcceptsScalarValue`

### Errors

All service-renderable errors implement:

```go
type Error interface {
	error
	Code() ErrorCode
}
```

Built-in errors include:

- `BadRequestError`
- `UnauthorizedError`
- `ForbiddenError`
- `InvalidIDError`
- `SingletonNotFoundError`
- `NotFoundError[IDT]`
- `NotAcceptableError`
- `NotDeletedError`
- `AlreadyUsedError`
- `SingletonAlreadyExistsError`
- `SingletonDeletedExistsError`
- `ValidationError`
- `ThrottledError`
- `InternalError`

`RenderError(e Error) (map[string]any, uint16)` converts a typed error into a
response body and status code.

## Storage Packages

Storage adapters and model fragments are documented in
[storage adapters](storage.md).

## `utils`

Public helpers:

- `NewFlags[T](values ...T) Flags[T]`
- `Flags.Has`, `Flags.Add`, `Flags.Del`
- `Validate(value any) error`
- `CheckPrefix(prefix string) error`
- `CheckURLArg(urlArg string) error`
