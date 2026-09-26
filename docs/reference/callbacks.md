# Callbacks and Middleware

Callbacks customize a `ResourceService` without replacing its endpoint logic.
They all receive `services.Context`, which hides the web framework while still
providing request data, response helpers, endpoint metadata, and the element
stack.

## `FilterFunc`

```go
type FilterFunc func(Context, *types.FilterExpression)
```

Adds request-specific restrictions before storage reads. Common uses:

- tenant scoping
- ownership checks
- visibility rules
- forcing a status or category filter

The callback mutates the passed filter. Use `FilterExpression.Restrict` to add
conditions with AND semantics.

## `AllowedFieldsFunc`

```go
type AllowedFieldsFunc func(Context) ([]string, Allowance)
```

Controls which JSON fields a caller may filter and sort by.

`Allowance` values:

- `All`: all valid mapped fields are allowed.
- `Only`: only the returned fields are allowed.
- `Except`: every valid mapped field except the returned fields is allowed.

## `ReaderFunc`

```go
type ReaderFunc[IDT comparable, RT types.Resource[IDT]] func(Context, *RT) error
```

Overrides request-body reading. The default reader requires `Content-Type:
application/json` and calls `Context.BindJSON`.

Use this callback when the request body is a DTO and not the storage model, or
when the API accepts a custom shape.

## `DefaultSortFunc`

```go
type DefaultSortFunc func(Context) types.SortExpression
```

Returns the list sort used when the request does not include `?sort=...`.
Singleton services ignore default sort.

## `ElementRendererFunc`

```go
type ElementRendererFunc[IDT comparable, RT types.Resource[IDT]] func(Context, RT) error
```

Renders one element. Use it to hide internal fields, embed links, or normalize
response bodies.

Without this callback, the service renders the resource directly as JSON.

## `PageRendererFunc`

```go
type PageRendererFunc[IDT comparable, RT types.Resource[IDT]] func(Context, []RT, int64, int64) error
```

Renders list responses. The arguments are context, elements, current page, and
total pages.

The default body is:

```json
{"elements":[],"page":0,"totalPages":0}
```

## `ValidatorFunc`

```go
type ValidatorFunc[IDT comparable, RT types.Resource[IDT]] func(Context, RT) error
```

Validates a resource before saving. Return `nil` when valid. Return
`types.ValidationError` for field validation failures:

```go
return types.ValidationError{
	Errors: map[string]any{"name": "required"},
}
```

## `MiddlewareFunc`

```go
type HandlerFunc func(Context) error
type MiddlewareFunc func(next HandlerFunc) HandlerFunc
```

Middleware is framework-neutral. HTTP adapters wrap it into the target
framework's middleware or handler chain when installing routes.

Service middleware runs after endpoint setup middleware. Element routes also run
the element middleware that loads the current resource and pushes it onto the
context stack.

## Context Data and Element Stack

Middleware can pass values to handlers with:

```go
context.SetData("key", value)
value, ok := context.GetData("key")
```

Nested resource routes push parent elements first, then current elements. Use
`PeekElement(0)` for the current element and higher indexes for parents.
