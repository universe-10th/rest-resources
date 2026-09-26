// Package resources is the module root for rest-resources.
//
// Most functionality lives in subpackages:
//   - echo installs resource services into Echo apps and groups.
//   - fiber installs resource services into Fiber apps and groups.
//   - chi installs resource services into Chi routers.
//   - encore exposes a net/http handler for Encore raw endpoints.
//   - types defines resource, storage, query, mapping, and error contracts.
//   - types/services builds typed CRUD resource services.
//   - memory, gorm, and mongo provide storage adapters and model fragments.
package resources
