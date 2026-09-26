# Tutorial

This tutorial builds a small API with stores and catalogs. Stores are
soft-deletable. Catalogs are nested under stores.

## 1. Define Resource Models

Use model fragments from `memory`, `gorm/types`, or `mongo/types`, or implement
the `types.Resource` interfaces yourself.

```go
package main

import "github.com/universe-10th/rest-resources/memory"

type Store struct {
	memory.SoftDeletedResource[int]
	Name string `json:"name"`
}

type Catalog struct {
	memory.Resource[int]
	StoreID int    `json:"store_id"`
	Name    string `json:"name"`
}
```

`Store` supports deleted routes because it embeds `memory.SoftDeletedResource`.
`Catalog` is hard-deleted because it embeds `memory.Resource`.

## 2. Create Storage

For a prototype or test, use memory storage:

```go
storeStorage := memory.NewStorage[int, *Store]()
catalogStorage := memory.NewStorage[int, *Catalog]()
```

For production, use `gorm.NewStorage` or `mongo.NewStorage` with your database
connection. The service code stays the same because all adapters implement
`types.Storage`.

## 3. Create Services

```go
import "github.com/universe-10th/rest-resources/types/services"

stores := services.MustCreateCollectionService[int, *Store](
	"stores",
	"store_id",
	storeStorage,
)

catalogs := services.MustCreateCollectionService[int, *Catalog](
	"catalogs",
	"catalog_id",
	catalogStorage,
)

catalogs.MustAttachTo(stores, "store_id")
```

The third argument to `MustAttachTo` is the child JSON field constrained by the
parent ID. Here, every catalog under `/stores/:store_id/catalogs` receives or
uses `store_id`.

## 4. Configure Behavior

Configuration methods return the service, so they can be chained.

```go
stores.
	UsingPageSize(20).
	UsingDefaultSort(func(context services.Context) types.SortExpression {
		return types.SortExpression{
			Sort: []types.Sort{{Field: "name", Order: types.Asc}},
		}
	}).
	UsingAllowedFields(func(context services.Context) ([]string, services.Allowance) {
		return []string{"id", "name", "created_at"}, services.Only
	})
```

Useful callbacks include:

- `UsingReader` to read a custom request DTO.
- `UsingValidator` to run domain validation before saving.
- `UsingFilter` to apply tenant, ownership, or visibility constraints.
- `UsingElementRenderer` to project one element into a response DTO.
- `UsingPageRenderer` to control list response shape.
- `UsingMiddlewares` to run service-level middleware around every endpoint.
- `UsingVerbs` to restrict which standard routes are installed.

## 5. Install Into A Router

This example uses Echo:

```go
import (
	echov4 "github.com/labstack/echo/v4"
	resourceecho "github.com/universe-10th/rest-resources/echo"
)

app := echov4.New()
resourceecho.MustInstall(app, stores)
```

You only install the root service. Attached child services are installed under
their parent routes.

The same service tree can be installed with another adapter:

```go
import (
	resourcechi "github.com/universe-10th/rest-resources/chi"
	resourceencore "github.com/universe-10th/rest-resources/encore"
	resourcefiber "github.com/universe-10th/rest-resources/fiber"
)

resourcefiber.MustInstall(app, stores)
resourcechi.MustInstall(router, stores)
handler := resourceencore.MustNewHandler(stores)
```

Install into an Echo group when the API has a prefix or group middleware:

```go
api := app.Group("/api")
resourceecho.MustInstall(api, stores)
```

## 6. Try The Routes

Create a store:

```sh
curl -X POST http://localhost:8080/stores \
  -H 'Content-Type: application/json' \
  -d '{"name":"Main"}'
```

List stores:

```sh
curl 'http://localhost:8080/stores?sort=name&page=0'
```

Create a catalog under store `1`:

```sh
curl -X POST http://localhost:8080/stores/1/catalogs \
  -H 'Content-Type: application/json' \
  -d '{"name":"Fall"}'
```

Filter stores:

```sh
curl --get http://localhost:8080/stores \
  --data-urlencode 'filter={"name":{"$contains":"Main"}}'
```

Soft-delete and restore a store:

```sh
curl -X DELETE http://localhost:8080/stores/1
curl http://localhost:8080/stores/deleted
curl -X POST http://localhost:8080/stores/deleted/1
```

## 7. Custom Rendering Example

```go
stores.UsingElementRenderer(func(context services.Context, store *Store) error {
	return context.RenderJSON(200, map[string]any{
		"id":   store.ID,
		"name": store.Name,
	})
})

stores.UsingPageRenderer(func(
	context services.Context,
	stores []*Store,
	page int64,
	totalPages int64,
) error {
	return context.RenderJSON(200, map[string]any{
		"items": stores,
		"page":  page,
		"pages": totalPages,
	})
})
```

## 8. Validation Example

```go
stores.UsingValidator(func(context services.Context, store *Store) error {
	if store.Name == "" {
		return types.ValidationError{
			Errors: map[string]any{"name": "required"},
		}
	}
	return nil
})
```

Validation errors are rendered as `422` responses by the service layer.
