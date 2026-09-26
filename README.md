# rest-resources

`rest-resources` builds typed CRUD resource services for Go HTTP frameworks
such as Echo, Fiber, Chi, and Encore raw endpoints. You describe a resource, choose a storage adapter,
configure callbacks such as validation and rendering, and install the service
into a supported router.

The package focuses on APIs where collections, singleton resources, nested
resources, filters, sort, pagination, hard deletes, and soft deletes all follow
the same conventions.

## Documentation

- [Introduction and rationale](docs/introduction/README.md)
- [Tutorial](docs/tutorial/README.md)
- [Full API documentation](docs/reference/README.md)
- [Filter and sort syntax](docs/reference/queries.md)

## Requirements

- Go 1.25 or newer
- Echo v4, Fiber v3, Chi v5, or Encore raw endpoints, depending on the adapter
  you use

The module uses Go toolchain support, so older compatible Go installations can
download the required toolchain automatically when `GOTOOLCHAIN=auto` is enabled.

## Installation

```sh
go get github.com/universe-10th/rest-resources
```

## Quick Start

This is a quick start with `Echo`. The same service can also be installed into
Fiber, Chi, or an Encore raw endpoint handler.

```go
package main

import (
	echov4 "github.com/labstack/echo/v4"
	resourceecho "github.com/universe-10th/rest-resources/echo"
	"github.com/universe-10th/rest-resources/memory"
	"github.com/universe-10th/rest-resources/types/services"
)

type Book struct {
	memory.Resource[int]
	Title string `json:"title"`
}

func main() {
	app := echov4.New()

	storage := memory.NewStorage[int, *Book]()
	books := services.MustCreateCollectionService[int, *Book]("books", "book_id", storage)

	resourceecho.MustInstall(app, books)
	app.Logger.Fatal(app.Start(":8080"))
}
```

This registers the default collection routes:

```text
GET    /books
POST   /books
GET    /books/:book_id
PATCH  /books/:book_id
DELETE /books/:book_id
```

Install into a subgroup when your API has a prefix:

```go
api := app.Group("/api")
resourceecho.MustInstall(api, books)
```

Other adapters:

```go
import (
	resourcechi "github.com/universe-10th/rest-resources/chi"
	resourceencore "github.com/universe-10th/rest-resources/encore"
	resourcefiber "github.com/universe-10th/rest-resources/fiber"
)

resourcefiber.MustInstall(app, books)      // *fiber.App or fiber.Router
resourcechi.MustInstall(router, books)     // chi.Router
handler := resourceencore.MustNewHandler(books) // use from an Encore raw endpoint
```

## Package Map

- `echo`: Echo adapter, context wrapper, and service installer.
- `fiber`: Fiber adapter, context wrapper, and service installer.
- `chi`: Chi adapter, context wrapper, and service installer.
- `encore`: `net/http` handler for Encore raw endpoints.
- `types`: framework-neutral resource, storage, filter, sort, mapping, and error
  contracts.
- `types/services`: resource service constructors, endpoint behavior,
  callbacks, middleware, and nesting.
- `memory`: in-process storage and reusable model fragments for tests and
  prototypes.
- `gorm`: GORM storage adapter.
- `gorm/types`: GORM-ready resource fragments, field mapping, filter, and sort
  serializers.
- `mongo`: MongoDB storage adapter.
- `mongo/types`: MongoDB-ready resource fragments, field mapping, filter, and
  sort serializers.
- `utils`: small validation and flag helpers used by the public service APIs.

## Development

```sh
go test ./...
go vet ./...
gofmt -w .
```

## License

MIT. See [LICENSE](LICENSE).
