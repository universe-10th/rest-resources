# Introduction and Rationale

`rest-resources` is a small resource-service layer for Go applications using
Echo and other supported libraries. It is not an ORM, and it is not a replacement
for those REST libraries. It sits between an HTTP router and a storage implementation
so common REST resource behavior can be declared once and reused consistently.

## What It Solves

Many JSON APIs repeat the same code for every resource:

- parse an ID from the path
- bind JSON bodies
- validate input
- apply tenant or parent-resource restrictions
- filter, sort, and paginate lists
- decide whether deletes are hard or soft
- render consistent success and error responses
- attach nested routes such as `/stores/:store_id/catalogs`

This project turns those behaviors into typed services. A service knows its URL
prefix, ID path parameter name, storage backend, supported verbs, callbacks, and
children. HTTP adapters install that service as concrete routes for Echo, Fiber,
Chi, or Encore raw endpoints.

## Design Goals

- Keep resource behavior framework-neutral where possible.
- Let Echo remain the HTTP framework and router.
- Support simple resources first, then allow nested and constrained resources.
- Keep storage behind a small interface so memory, GORM, MongoDB, and custom
  engines can share the same service layer.
- Make query input database-neutral: parse filters and sort once, then serialize
  them per storage engine.
- Let applications override reading, validation, filtering, rendering, and
  middleware without replacing the whole service.

## Core Concepts

Resource models implement `types.Resource[IDT]`. A soft-deletable model
implements `types.SoftDeletedResource[IDT]`.

Storage implements `types.Storage[IDT, RT]`. It receives parsed filters and sort
expressions and translates them into actual reads, writes, deletes, restores, and
prunes.

Services are created with `types/services` constructors:

- `MustCreateCollectionService(prefix, urlArg, storage)`
- `CreateCollectionService(prefix, urlArg, storage)`
- `MustCreateSingletonService(prefix, storage)`
- `CreateSingletonService(prefix, storage)`

HTTP adapters install only root services. Child services are attached to parents
with `AttachTo` or `MustAttachTo`, then installed automatically under their
parent element routes.

## Route Shape

A collection service named `books` with `book_id` registers:

```text
GET    /books
POST   /books
GET    /books/:book_id
PATCH  /books/:book_id
DELETE /books/:book_id
```

When the resource is soft-deletable, deleted-resource routes are also available:

```text
GET    /books/deleted
GET    /books/deleted/:book_id
POST   /books/deleted/:book_id
DELETE /books/deleted/:book_id
```

A singleton service named `settings` registers element routes without an ID:

```text
POST   /settings
GET    /settings
PATCH  /settings
DELETE /settings
```

Soft-deletable singletons also get:

```text
GET    /settings/deleted
POST   /settings/deleted
DELETE /settings/deleted
```

## Nesting Rationale

Nested resources are modeled as real services, not as ad hoc route handlers.
For example:

```go
catalogs.MustAttachTo(stores, "store_id")
products.MustAttachTo(catalogs, "catalog_id")
```

This yields routes like:

```text
/stores/:store_id/catalogs
/stores/:store_id/catalogs/:catalog_id/products
```

The child service gets constraints from parent path elements. A catalog created
under `/stores/1/catalogs` is constrained by the JSON field `store_id`. The
service stack also lets callbacks and middleware inspect the current parent
elements through `Context.PeekElement`.

## When To Use It

Use this project when you have several CRUD-like JSON resources and want one
consistent implementation for routing, query parsing, storage calls, and common
callbacks.

Use plain framework handlers when an endpoint is highly custom, not
resource-shaped, or should not follow the service conventions.
