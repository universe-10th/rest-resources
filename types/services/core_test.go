package services

import (
	"errors"
	"testing"
	"time"

	"github.com/universe-10th/rest-resources/types"
	"github.com/universe-10th/rest-resources/utils"
)

type coreConstraintResource struct {
	ID       int `json:"id"`
	ParentID int `json:"parent_id"`
}

func (r coreConstraintResource) GetID() int                         { return r.ID }
func (r coreConstraintResource) SetID(int)                          {}
func (r coreConstraintResource) GetIDField() string                 { return "id" }
func (r coreConstraintResource) GetCreationTime() time.Time         { return time.Time{} }
func (r coreConstraintResource) GetLastUpdateTime() time.Time       { return time.Time{} }
func (r coreConstraintResource) SetCreationTime()                   {}
func (r coreConstraintResource) SetCreationTimeIn(*time.Location)   {}
func (r coreConstraintResource) RestoreCreationTime(time.Time)      {}
func (r coreConstraintResource) SetLastUpdateTime()                 {}
func (r coreConstraintResource) SetLastUpdateTimeIn(*time.Location) {}
func (r coreConstraintResource) GetCreationTimeField() string       { return "created_at" }
func (r coreConstraintResource) GetLastUpdateTimeField() string     { return "updated_at" }

type coreConstraintParentResource struct {
	ID int `json:"id"`
}

func (r coreConstraintParentResource) GetID() int                         { return r.ID }
func (r coreConstraintParentResource) SetID(int)                          {}
func (r coreConstraintParentResource) GetIDField() string                 { return "id" }
func (r coreConstraintParentResource) GetCreationTime() time.Time         { return time.Time{} }
func (r coreConstraintParentResource) GetLastUpdateTime() time.Time       { return time.Time{} }
func (r coreConstraintParentResource) SetCreationTime()                   {}
func (r coreConstraintParentResource) SetCreationTimeIn(*time.Location)   {}
func (r coreConstraintParentResource) RestoreCreationTime(time.Time)      {}
func (r coreConstraintParentResource) SetLastUpdateTime()                 {}
func (r coreConstraintParentResource) SetLastUpdateTimeIn(*time.Location) {}
func (r coreConstraintParentResource) GetCreationTimeField() string       { return "created_at" }
func (r coreConstraintParentResource) GetLastUpdateTimeField() string     { return "updated_at" }

type coreConstraintBadResource struct {
	ID       int    `json:"id"`
	ParentID string `json:"parent_id"`
}

func (r coreConstraintBadResource) GetID() int                         { return r.ID }
func (r coreConstraintBadResource) SetID(int)                          {}
func (r coreConstraintBadResource) GetIDField() string                 { return "id" }
func (r coreConstraintBadResource) GetCreationTime() time.Time         { return time.Time{} }
func (r coreConstraintBadResource) GetLastUpdateTime() time.Time       { return time.Time{} }
func (r coreConstraintBadResource) SetCreationTime()                   {}
func (r coreConstraintBadResource) SetCreationTimeIn(*time.Location)   {}
func (r coreConstraintBadResource) RestoreCreationTime(time.Time)      {}
func (r coreConstraintBadResource) SetLastUpdateTime()                 {}
func (r coreConstraintBadResource) SetLastUpdateTimeIn(*time.Location) {}
func (r coreConstraintBadResource) GetCreationTimeField() string       { return "created_at" }
func (r coreConstraintBadResource) GetLastUpdateTimeField() string     { return "updated_at" }

func TestReadBindsJSONBody(t *testing.T) {
	t.Parallel()

	var element coreConstraintResource
	context := coreConstraintContext{
		contentType: "application/json; charset=utf-8",
		bind: func(target any) error {
			resource := target.(*coreConstraintResource)
			resource.ID = 42
			return nil
		},
	}
	service := ResourceService[int, coreConstraintResource]{}

	err := service.read(context, &element)
	if err != nil {
		t.Fatalf("read returned error: %v", err)
	}
	if element.ID != 42 {
		t.Fatalf("expected ID 42, got %d", element.ID)
	}
}

func TestReadUsesCustomReader(t *testing.T) {
	t.Parallel()

	var element coreConstraintResource
	service := ResourceService[int, coreConstraintResource]{}
	service.UsingReader(
		func(context Context, element *coreConstraintResource) error {
			element.ID = 84
			return nil
		},
	)

	err := service.read(coreConstraintContext{contentType: "application/json"}, &element)
	if err != nil {
		t.Fatalf("read returned error: %v", err)
	}
	if element.ID != 84 {
		t.Fatalf("expected ID 84, got %d", element.ID)
	}
}

func TestReadRejectsNonJSONContent(t *testing.T) {
	t.Parallel()

	var element coreConstraintResource
	service := ResourceService[int, coreConstraintResource]{}

	err := service.read(coreConstraintContext{contentType: "text/plain"}, &element)
	var badRequest types.BadRequestError
	if !errors.As(err, &badRequest) {
		t.Fatalf("expected BadRequestError, got %T: %v", err, err)
	}
}

func TestMustAttachToLinksParentAndChild(t *testing.T) {
	t.Parallel()

	parent := ResourceService[int, coreConstraintParentResource]{
		prefix:  "parents",
		urlArg:  "parent_id",
		storage: newCoreConstraintStorage[int, coreConstraintParentResource](),
	}
	child := ResourceService[int, coreConstraintResource]{
		prefix:  "children",
		urlArg:  "child_id",
		storage: newCoreConstraintStorage[int, coreConstraintResource](),
	}

	child.MustAttachTo(&parent, "parent_id")

	if child.Parent() != &parent {
		t.Fatal("expected child parent to be set")
	}
	if child.constraintJSONField != parent.URLArg() {
		t.Fatalf("expected constraint field %q, got %q", parent.URLArg(), child.constraintJSONField)
	}

	children := parent.Children()
	if len(children) != 1 || children[0] != &child {
		t.Fatalf("expected parent to have child registered, got %#v", children)
	}
}

func TestAddElementExtraRegistersAndForcesGetForExplicitVerbs(t *testing.T) {
	t.Parallel()

	service := ResourceService[int, coreConstraintParentResource]{
		prefix:  "parents",
		urlArg:  "parent_id",
		storage: newCoreConstraintStorage[int, coreConstraintParentResource](),
	}
	service.UsingVerbs(ResourceList)

	err := service.AddElementExtra("post", "publish", func(Context) error { return nil })
	if err != nil {
		t.Fatalf("AddElementExtra returned error: %v", err)
	}

	extras := service.ElementExtras()
	if len(extras) != 1 || extras[0].Method != "POST" || extras[0].Name != "publish" {
		t.Fatalf("unexpected element extras: %#v", extras)
	}
	if !service.Verbs().Has(ResourceGet) {
		t.Fatal("expected ResourceGet to be forced for element extra")
	}
}

func TestAddCollectionExtraRegistersEndpoint(t *testing.T) {
	t.Parallel()

	service := ResourceService[int, coreConstraintParentResource]{
		prefix:  "parents",
		urlArg:  "parent_id",
		storage: newCoreConstraintStorage[int, coreConstraintParentResource](),
	}

	err := service.AddCollectionExtra("delete", "archive", func(Context) error { return nil })
	if err != nil {
		t.Fatalf("AddCollectionExtra returned error: %v", err)
	}

	extras := service.CollectionExtras()
	if len(extras) != 1 || extras[0].Method != "DELETE" || extras[0].Name != "archive" {
		t.Fatalf("unexpected collection extras: %#v", extras)
	}
}

func TestAddCollectionExtraRejectsSingleton(t *testing.T) {
	t.Parallel()

	service := ResourceService[int, coreConstraintParentResource]{
		prefix:    "platform",
		singleton: true,
		storage:   newCoreConstraintStorage[int, coreConstraintParentResource](),
	}

	err := service.AddCollectionExtra("GET", "statistics", func(Context) error { return nil })
	if !errors.Is(err, ErrCollectionExtraOnSingleton) {
		t.Fatalf("expected ErrCollectionExtraOnSingleton, got %v", err)
	}
}

func TestAddExtraRejectsDuplicates(t *testing.T) {
	t.Parallel()

	service := ResourceService[int, coreConstraintParentResource]{
		prefix:  "parents",
		urlArg:  "parent_id",
		storage: newCoreConstraintStorage[int, coreConstraintParentResource](),
	}

	service.MustAddElementExtra("GET", "publish", func(Context) error { return nil })
	if err := service.AddElementExtra("get", "publish", func(Context) error { return nil }); !errors.Is(err, ErrConflictingExtraEndpoint) {
		t.Fatalf("expected ErrConflictingExtraEndpoint, got %v", err)
	}

	service.MustAddCollectionExtra("POST", "search", func(Context) error { return nil })
	if err := service.AddCollectionExtra("post", "search", func(Context) error { return nil }); !errors.Is(err, ErrConflictingExtraEndpoint) {
		t.Fatalf("expected ErrConflictingExtraEndpoint, got %v", err)
	}
}

func TestAddExtraRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	service := ResourceService[int, coreConstraintParentResource]{
		prefix:  "parents",
		urlArg:  "parent_id",
		storage: newCoreConstraintStorage[int, coreConstraintParentResource](),
	}

	if err := service.AddElementExtra("TRACE", "publish", func(Context) error { return nil }); !errors.Is(err, ErrInvalidExtraEndpointMethod) {
		t.Fatalf("expected ErrInvalidExtraEndpointMethod, got %v", err)
	}
	if err := service.AddElementExtra("GET", "not valid", func(Context) error { return nil }); !errors.Is(err, utils.ErrInvalidPrefix) {
		t.Fatalf("expected ErrInvalidPrefix, got %v", err)
	}
	if err := service.AddElementExtra("GET", "publish", nil); !errors.Is(err, ErrInvalidExtraEndpointHandler) {
		t.Fatalf("expected ErrInvalidExtraEndpointHandler, got %v", err)
	}
	if err := service.AddCollectionExtra("OPTIONS", "search", func(Context) error { return nil }); !errors.Is(err, ErrInvalidExtraEndpointMethod) {
		t.Fatalf("expected ErrInvalidExtraEndpointMethod, got %v", err)
	}
	if err := service.AddCollectionExtra("GET", "not valid", func(Context) error { return nil }); !errors.Is(err, utils.ErrInvalidPrefix) {
		t.Fatalf("expected ErrInvalidPrefix, got %v", err)
	}
	if err := service.AddCollectionExtra("GET", "search", nil); !errors.Is(err, ErrInvalidExtraEndpointHandler) {
		t.Fatalf("expected ErrInvalidExtraEndpointHandler, got %v", err)
	}
}

func TestExtraAccessorsReturnCopies(t *testing.T) {
	t.Parallel()

	service := ResourceService[int, coreConstraintParentResource]{
		prefix:  "parents",
		urlArg:  "parent_id",
		storage: newCoreConstraintStorage[int, coreConstraintParentResource](),
	}
	service.MustAddElementExtra("GET", "publish", func(Context) error { return nil })
	service.MustAddCollectionExtra("GET", "search", func(Context) error { return nil })

	elements := service.ElementExtras()
	elements[0].Name = "changed"
	collections := service.CollectionExtras()
	collections[0].Name = "changed"

	if service.ElementExtras()[0].Name != "publish" {
		t.Fatalf("element extras accessor exposed internal slice: %#v", service.ElementExtras())
	}
	if service.CollectionExtras()[0].Name != "search" {
		t.Fatalf("collection extras accessor exposed internal slice: %#v", service.CollectionExtras())
	}
}

func TestElementExtraConflictsWithChildPrefix(t *testing.T) {
	t.Parallel()

	parent := ResourceService[int, coreConstraintParentResource]{
		prefix:  "parents",
		urlArg:  "parent_id",
		storage: newCoreConstraintStorage[int, coreConstraintParentResource](),
	}
	child := ResourceService[int, coreConstraintResource]{
		prefix:  "publish",
		urlArg:  "child_id",
		storage: newCoreConstraintStorage[int, coreConstraintResource](),
	}

	child.MustAttachTo(&parent, "parent_id")

	err := parent.AddElementExtra("GET", "publish", func(Context) error { return nil })
	if !errors.Is(err, ErrConflictingExtraEndpoint) {
		t.Fatalf("expected ErrConflictingExtraEndpoint, got %v", err)
	}
}

func TestChildPrefixConflictsWithElementExtra(t *testing.T) {
	t.Parallel()

	parent := ResourceService[int, coreConstraintParentResource]{
		prefix:  "parents",
		urlArg:  "parent_id",
		storage: newCoreConstraintStorage[int, coreConstraintParentResource](),
	}
	parent.MustAddElementExtra("GET", "publish", func(Context) error { return nil })

	child := ResourceService[int, coreConstraintResource]{
		prefix:  "publish",
		urlArg:  "child_id",
		storage: newCoreConstraintStorage[int, coreConstraintResource](),
	}

	err := child.AttachTo(&parent, "parent_id")
	if !errors.Is(err, ErrConflictingExtraEndpoint) {
		t.Fatalf("expected ErrConflictingExtraEndpoint, got %v", err)
	}
	if child.Parent() != nil {
		t.Fatal("expected failed attachment not to set parent")
	}
}

func TestAttachToReturnsErrorForCycle(t *testing.T) {
	t.Parallel()

	parent := ResourceService[int, coreConstraintParentResource]{
		prefix:  "parents",
		urlArg:  "parent_id",
		storage: newCoreConstraintStorage[int, coreConstraintParentResource](),
	}
	child := ResourceService[int, coreConstraintResource]{
		prefix:  "children",
		urlArg:  "child_id",
		storage: newCoreConstraintStorage[int, coreConstraintResource](),
	}

	child.MustAttachTo(&parent, "parent_id")

	err := parent.AttachTo(&child, "parent_id")
	if !errors.Is(err, ErrCyclicServiceAttachment) {
		t.Fatalf("expected ErrCyclicServiceAttachment, got %v", err)
	}
}

func TestAttachToReturnsErrorForDuplicateURLArgInParentPath(t *testing.T) {
	t.Parallel()

	parent := ResourceService[int, coreConstraintParentResource]{
		prefix:  "parents",
		urlArg:  "id",
		storage: newCoreConstraintStorage[int, coreConstraintParentResource](),
	}
	child := ResourceService[int, coreConstraintResource]{
		prefix: "children",
		urlArg: "id",
	}

	err := child.AttachTo(&parent, "parent_id")
	if !errors.Is(err, ErrConflictingServiceURLArg) {
		t.Fatalf("expected ErrConflictingServiceURLArg, got %v", err)
	}
}

func TestAttachToReturnsErrorForInvalidConstraintJSONField(t *testing.T) {
	t.Parallel()

	parent := ResourceService[int, coreConstraintParentResource]{
		prefix:  "parents",
		urlArg:  "parent_id",
		storage: newCoreConstraintStorage[int, coreConstraintParentResource](),
	}
	child := ResourceService[int, coreConstraintResource]{
		prefix:  "children",
		urlArg:  "child_id",
		storage: newCoreConstraintStorage[int, coreConstraintResource](),
	}

	err := child.AttachTo(&parent, "missing")
	if !errors.Is(err, ErrInvalidConstraintJSONField) {
		t.Fatalf("expected ErrInvalidConstraintJSONField, got %v", err)
	}
}

func TestAttachToReturnsErrorForConstraintIDTypeMismatch(t *testing.T) {
	t.Parallel()

	parent := ResourceService[int, coreConstraintParentResource]{
		prefix:  "parents",
		urlArg:  "parent_id",
		storage: newCoreConstraintStorage[int, coreConstraintParentResource](),
	}
	child := ResourceService[int, coreConstraintBadResource]{
		prefix:  "children",
		urlArg:  "child_id",
		storage: newCoreConstraintStorage[int, coreConstraintBadResource](),
	}

	err := child.AttachTo(&parent, "parent_id")
	if !errors.Is(err, ErrInvalidConstraintIDType) {
		t.Fatalf("expected ErrInvalidConstraintIDType, got %v", err)
	}
}

func TestApplyPreviousConstraintSetsMappedField(t *testing.T) {
	t.Parallel()

	parent := coreConstraintParentResource{ID: 42}
	element := coreConstraintResource{ID: 100}
	service := ResourceService[int, coreConstraintResource]{
		prefix:              "children",
		storage:             newCoreConstraintStorage[int, coreConstraintResource](),
		constraintJSONField: "parent_id",
	}

	err := service.applyPreviousConstraint(coreConstraintContext{element: parent}, &element)
	if err != nil {
		t.Fatalf("applyPreviousConstraint returned error: %v", err)
	}
	if element.ParentID != parent.ID {
		t.Fatalf("expected ParentID %d, got %d", parent.ID, element.ParentID)
	}
}

func TestApplyConstraintReturnsInternalErrorForUnmappedField(t *testing.T) {
	t.Parallel()

	parent := coreConstraintParentResource{ID: 42}
	element := coreConstraintResource{ID: 100}
	service := ResourceService[int, coreConstraintResource]{
		prefix:              "children",
		storage:             newCoreConstraintStorage[int, coreConstraintResource](),
		constraintJSONField: "missing",
	}

	err := service.applyPreviousConstraint(coreConstraintContext{element: parent}, &element)
	var internal types.InternalError
	if !errors.As(err, &internal) {
		t.Fatalf("expected InternalError, got %T: %v", err, err)
	}
}

func TestApplyConstraintReturnsInternalErrorForIncompatibleField(t *testing.T) {
	t.Parallel()

	parent := coreConstraintParentResource{ID: 42}
	element := coreConstraintBadResource{ID: 100}
	service := ResourceService[int, coreConstraintBadResource]{
		prefix:              "children",
		storage:             newCoreConstraintStorage[int, coreConstraintBadResource](),
		constraintJSONField: "parent_id",
	}

	err := service.applyPreviousConstraint(coreConstraintContext{element: parent}, &element)
	var internal types.InternalError
	if !errors.As(err, &internal) {
		t.Fatalf("expected InternalError, got %T: %v", err, err)
	}
}

func TestUpdateReadsRestoresAppliesConstraintSavesAndRenders(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)
	bodyCreatedAt := createdAt.Add(time.Hour)
	parent := &endpointTestResource{ID: 7}
	current := &endpointTestResource{ID: 42, CreatedAt: createdAt}
	storage := newEndpointStorage()
	context := &endpointTestContext{
		stack:       []any{current, parent},
		contentType: "application/json",
		bind: func(target any) error {
			element := target.(**endpointTestResource)
			*element = &endpointTestResource{ID: 999, CreatedAt: bodyCreatedAt, Name: "updated"}
			return nil
		},
	}
	service := ResourceService[int, *endpointTestResource]{
		prefix:              "children",
		storage:             storage,
		constraintJSONField: "parent_id",
		validator: func(Context, *endpointTestResource) error {
			storage.calls = append(storage.calls, "Validate")
			return nil
		},
	}

	err := service.Update(context)
	if err != nil {
		t.Fatalf("update returned error: %v", err)
	}
	if storage.saved == nil {
		t.Fatal("expected update to save element")
	}
	if storage.saved.ID != current.ID {
		t.Fatalf("expected ID %d, got %d", current.ID, storage.saved.ID)
	}
	if !storage.saved.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected CreatedAt %s, got %s", createdAt, storage.saved.CreatedAt)
	}
	if storage.saved.ParentID != parent.ID {
		t.Fatalf("expected ParentID %d, got %d", parent.ID, storage.saved.ParentID)
	}
	if context.renderStatus != 200 {
		t.Fatalf("expected render status 200, got %d", context.renderStatus)
	}
}

func TestCreateClearsIDStampsSavesAndRenders(t *testing.T) {
	t.Parallel()

	storage := newEndpointStorage()
	context := &endpointTestContext{
		contentType: "application/json",
		bind: func(target any) error {
			element := target.(**endpointTestResource)
			*element = &endpointTestResource{ID: 999, Name: "created"}
			return nil
		},
	}
	service := ResourceService[int, *endpointTestResource]{
		prefix:  "resources",
		storage: storage,
	}

	err := service.Create(context)
	if err != nil {
		t.Fatalf("create returned error: %v", err)
	}
	if storage.saved == nil {
		t.Fatal("expected create to save element")
	}
	if storage.saved.ID != 0 {
		t.Fatalf("expected cleared ID 0, got %d", storage.saved.ID)
	}
	if storage.saved.CreatedAt.IsZero() {
		t.Fatal("expected creation time to be set")
	}
	if context.renderStatus != 201 {
		t.Fatalf("expected render status 201, got %d", context.renderStatus)
	}
}

func TestCreateSingletonRejectsExistingActiveElement(t *testing.T) {
	t.Parallel()

	storage := newEndpointStorage()
	storage.getResults = []endpointGetResult{
		{element: &endpointTestResource{ID: 1}, found: true},
	}
	context := &endpointTestContext{contentType: "application/json"}
	service := ResourceService[int, *endpointTestResource]{
		prefix:    "profile",
		storage:   storage,
		singleton: true,
	}

	err := service.Create(context)
	if err != nil {
		t.Fatalf("create returned error: %v", err)
	}
	if context.renderStatus != int(types.ErrConflict) {
		t.Fatalf("expected conflict status, got %d", context.renderStatus)
	}
	if storage.saved != nil {
		t.Fatal("expected singleton conflict to skip save")
	}
}

func TestDeletePruneAndRestoreUseStackedElement(t *testing.T) {
	t.Parallel()

	element := &endpointTestResource{ID: 42}

	deleteStorage := newEndpointStorage()
	deleteContext := &endpointTestContext{stack: []any{element}}
	deleteService := ResourceService[int, *endpointTestResource]{
		prefix:  "resources",
		storage: deleteStorage,
	}
	if err := deleteService.Delete(deleteContext); err != nil {
		t.Fatalf("delete returned error: %v", err)
	}
	if deleteStorage.deleted != element {
		t.Fatal("expected delete to use stacked element")
	}
	if deleteContext.noContentStatus != 204 {
		t.Fatalf("expected delete status 204, got %d", deleteContext.noContentStatus)
	}

	pruneStorage := newEndpointStorage()
	pruneContext := &endpointTestContext{stack: []any{element}}
	pruneService := ResourceService[int, *endpointTestResource]{
		prefix:  "resources",
		storage: pruneStorage,
	}
	if err := pruneService.Prune(pruneContext); err != nil {
		t.Fatalf("prune returned error: %v", err)
	}
	if pruneStorage.pruned != element {
		t.Fatal("expected prune to use stacked element")
	}
	if pruneContext.noContentStatus != 204 {
		t.Fatalf("expected prune status 204, got %d", pruneContext.noContentStatus)
	}

	restoreStorage := newEndpointStorage()
	restoreContext := &endpointTestContext{stack: []any{element}}
	restoreService := ResourceService[int, *endpointTestResource]{
		prefix:  "resources",
		storage: restoreStorage,
	}
	if err := restoreService.Restore(restoreContext); err != nil {
		t.Fatalf("restore returned error: %v", err)
	}
	if restoreStorage.restored != element {
		t.Fatal("expected restore to use stacked element")
	}
	if restoreContext.renderStatus != 200 {
		t.Fatalf("expected restore status 200, got %d", restoreContext.renderStatus)
	}
}

func TestListParsesQueryAppliesRestrictionsRetrievesAndRenders(t *testing.T) {
	t.Parallel()

	parent := &endpointTestResource{ID: 7}
	storage := newEndpointStorage()
	storage.elements = []*endpointTestResource{
		{ID: 10, ParentID: parent.ID},
		{ID: 11, ParentID: parent.ID},
	}
	storage.total = 21
	context := &endpointTestContext{
		stack: []any{parent},
		query: map[string]string{
			"filter": `{"name":{"$contains":"a"}}`,
			"sort":   "-name",
			"page":   "2",
		},
	}
	service := ResourceService[int, *endpointTestResource]{
		prefix:              "children",
		storage:             storage,
		constraintJSONField: "parent_id",
		pageSize:            10,
		allowedFields: func(Context) ([]string, Allowance) {
			return []string{"name", "parent_id"}, Only
		},
	}

	err := service.List(context, true)
	if err != nil {
		t.Fatalf("list returned error: %v", err)
	}
	if storage.skip != 20 {
		t.Fatalf("expected skip 20, got %d", storage.skip)
	}
	if storage.limit != 10 {
		t.Fatalf("expected limit 10, got %d", storage.limit)
	}
	if storage.filter == nil {
		t.Fatal("expected filter to be passed to storage")
	}
	if storage.sort == nil || len(storage.sort.Sort) != 1 || storage.sort.Sort[0].Field != "name" || storage.sort.Sort[0].Order != types.Desc {
		t.Fatalf("unexpected sort: %#v", storage.sort)
	}
	if context.renderStatus != 200 {
		t.Fatalf("expected render status 200, got %d", context.renderStatus)
	}
	if context.renderBody == nil {
		t.Fatal("expected rendered page body")
	}
}

func TestListUsesDefaultSortAndDefaultPage(t *testing.T) {
	t.Parallel()

	storage := newEndpointStorage()
	context := &endpointTestContext{}
	service := ResourceService[int, *endpointTestResource]{
		prefix:  "resources",
		storage: storage,
		defaultSort: func(Context) types.SortExpression {
			return types.SortExpression{
				Sort: []types.Sort{{Field: "id", Order: types.Asc}},
			}
		},
	}

	err := service.List(context, false)
	if err != nil {
		t.Fatalf("list returned error: %v", err)
	}
	if storage.skip != 0 {
		t.Fatalf("expected default skip 0, got %d", storage.skip)
	}
	if storage.limit != defaultPageSize {
		t.Fatalf("expected default limit %d, got %d", defaultPageSize, storage.limit)
	}
	if storage.sort == nil || len(storage.sort.Sort) != 1 || storage.sort.Sort[0].Field != "id" {
		t.Fatalf("unexpected default sort: %#v", storage.sort)
	}
}

func TestListRejectsDisallowedFilterField(t *testing.T) {
	t.Parallel()

	storage := newEndpointStorage()
	context := &endpointTestContext{
		query: map[string]string{
			"filter": `{"name":{"$contains":"a"}}`,
		},
	}
	service := ResourceService[int, *endpointTestResource]{
		prefix:  "resources",
		storage: storage,
		allowedFields: func(Context) ([]string, Allowance) {
			return []string{"name"}, Except
		},
	}

	err := service.List(context, false)
	if err != nil {
		t.Fatalf("list returned error: %v", err)
	}
	if context.renderStatus != int(types.ErrBadRequest) {
		t.Fatalf("expected bad request status, got %d", context.renderStatus)
	}
}

type coreConstraintStorage[IDT comparable, RT types.Resource[IDT]] struct {
	mapping *types.FieldsMapping
}

func newCoreConstraintStorage[IDT comparable, RT types.Resource[IDT]]() coreConstraintStorage[IDT, RT] {
	return coreConstraintStorage[IDT, RT]{
		mapping: types.NewFieldsMapping[IDT, RT](func(any) map[string]string {
			return map[string]string{
				"ID":       "id",
				"ParentID": "parent_id",
			}
		}),
	}
}

func (s coreConstraintStorage[IDT, RT]) Mapping() *types.FieldsMapping { return s.mapping }
func (s coreConstraintStorage[IDT, RT]) GetElement(*types.FilterExpression) (RT, bool, error) {
	var zero RT
	return zero, false, nil
}
func (s coreConstraintStorage[IDT, RT]) GetElements(*types.FilterExpression, *types.SortExpression, int64, int64) ([]RT, int64, error) {
	return nil, 0, nil
}
func (s coreConstraintStorage[IDT, RT]) Save(*RT) (bool, error)    { return false, nil }
func (s coreConstraintStorage[IDT, RT]) Delete(*RT) (bool, error)  { return false, nil }
func (s coreConstraintStorage[IDT, RT]) Restore(*RT) (bool, error) { return false, nil }
func (s coreConstraintStorage[IDT, RT]) Prune(*RT) (bool, error)   { return false, nil }
func (s coreConstraintStorage[IDT, RT]) ValidateFilter(*types.FilterExpression) error {
	return nil
}
func (s coreConstraintStorage[IDT, RT]) ValidateSort(*types.SortExpression) error {
	return nil
}
func (s coreConstraintStorage[IDT, RT]) AddIDFilter(*types.FilterExpression, IDT)       {}
func (s coreConstraintStorage[IDT, RT]) AddDeletedFilter(*types.FilterExpression, bool) {}

type endpointTestResource struct {
	ID        int
	ParentID  int `json:"parent_id"`
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (r *endpointTestResource) GetID() int                          { return r.ID }
func (r *endpointTestResource) SetID(id int)                        { r.ID = id }
func (r *endpointTestResource) GetIDField() string                  { return "id" }
func (r *endpointTestResource) GetCreationTime() time.Time          { return r.CreatedAt }
func (r *endpointTestResource) GetLastUpdateTime() time.Time        { return r.UpdatedAt }
func (r *endpointTestResource) SetCreationTime()                    { r.CreatedAt = time.Now().UTC() }
func (r *endpointTestResource) SetCreationTimeIn(*time.Location)    { r.SetCreationTime() }
func (r *endpointTestResource) RestoreCreationTime(stamp time.Time) { r.CreatedAt = stamp }
func (r *endpointTestResource) SetLastUpdateTime()                  { r.UpdatedAt = time.Now().UTC() }
func (r *endpointTestResource) SetLastUpdateTimeIn(*time.Location)  { r.SetLastUpdateTime() }
func (r *endpointTestResource) GetCreationTimeField() string        { return "created_at" }
func (r *endpointTestResource) GetLastUpdateTimeField() string      { return "updated_at" }

type endpointGetResult struct {
	element *endpointTestResource
	found   bool
	err     error
}

type endpointStorage struct {
	mapping    *types.FieldsMapping
	getResults []endpointGetResult
	elements   []*endpointTestResource
	total      int64
	filter     *types.FilterExpression
	sort       *types.SortExpression
	skip       int64
	limit      int64
	saved      *endpointTestResource
	deleted    *endpointTestResource
	restored   *endpointTestResource
	pruned     *endpointTestResource
	calls      []string
}

func newEndpointStorage() *endpointStorage {
	return &endpointStorage{
		mapping: types.NewFieldsMapping[int, *endpointTestResource](func(any) map[string]string {
			return map[string]string{
				"ID":       "id",
				"ParentID": "parent_id",
			}
		}),
	}
}

func (s *endpointStorage) Mapping() *types.FieldsMapping { return s.mapping }
func (s *endpointStorage) GetElement(*types.FilterExpression) (*endpointTestResource, bool, error) {
	s.calls = append(s.calls, "GetElement")
	if len(s.getResults) == 0 {
		return nil, false, nil
	}

	result := s.getResults[0]
	s.getResults = s.getResults[1:]
	return result.element, result.found, result.err
}
func (s *endpointStorage) GetElements(filter *types.FilterExpression, sort *types.SortExpression, skip int64, limit int64) ([]*endpointTestResource, int64, error) {
	s.calls = append(s.calls, "GetElements")
	s.filter = filter
	s.sort = sort
	s.skip = skip
	s.limit = limit
	return s.elements, s.total, nil
}
func (s *endpointStorage) Save(element **endpointTestResource) (bool, error) {
	s.calls = append(s.calls, "Save")
	s.saved = *element
	return false, nil
}
func (s *endpointStorage) Delete(element **endpointTestResource) (bool, error) {
	s.calls = append(s.calls, "Delete")
	s.deleted = *element
	return false, nil
}
func (s *endpointStorage) Restore(element **endpointTestResource) (bool, error) {
	s.calls = append(s.calls, "Restore")
	s.restored = *element
	return false, nil
}
func (s *endpointStorage) Prune(element **endpointTestResource) (bool, error) {
	s.calls = append(s.calls, "Prune")
	s.pruned = *element
	return false, nil
}
func (s *endpointStorage) ValidateFilter(*types.FilterExpression) error {
	return nil
}
func (s *endpointStorage) ValidateSort(*types.SortExpression) error {
	return nil
}
func (s *endpointStorage) AddIDFilter(*types.FilterExpression, int) {}
func (s *endpointStorage) AddDeletedFilter(*types.FilterExpression, bool) {
	s.calls = append(s.calls, "AddDeletedFilter")
}

type endpointTestContext struct {
	stack           []any
	query           map[string]string
	contentType     string
	bind            func(any) error
	renderStatus    int
	renderBody      any
	noContentStatus int
}

func (c *endpointTestContext) Native() any                         { return nil }
func (c *endpointTestContext) GetPathParam(string) (string, error) { return "", nil }
func (c *endpointTestContext) GetQueryParam(name string) (string, error) {
	return c.query[name], nil
}
func (c *endpointTestContext) GetQueryParams(string) ([]string, error) { return nil, nil }
func (c *endpointTestContext) GetHeader(name string) (string, error) {
	if name == "Content-Type" {
		return c.contentType, nil
	}
	return "", nil
}
func (c *endpointTestContext) GetHeaders(string) ([]string, error) { return nil, nil }
func (c *endpointTestContext) GetCookie(string) (Cookie, error)    { return Cookie{}, nil }
func (c *endpointTestContext) BindJSON(target any) error {
	if c.bind != nil {
		return c.bind(target)
	}
	return nil
}
func (c *endpointTestContext) SetHeader(string, string)   {}
func (c *endpointTestContext) SetCookie(Cookie)           {}
func (c *endpointTestContext) GetData(string) (any, bool) { return nil, false }
func (c *endpointTestContext) SetData(string, any)        {}
func (c *endpointTestContext) PushElement(element any) {
	c.stack = append([]any{element}, c.stack...)
}
func (c *endpointTestContext) PopElement() (any, bool) {
	if len(c.stack) == 0 {
		return nil, false
	}
	element := c.stack[0]
	c.stack = c.stack[1:]
	return element, true
}
func (c *endpointTestContext) PeekElement(index int) (any, bool) {
	if index < 0 || index >= len(c.stack) {
		return nil, false
	}
	return c.stack[index], true
}
func (c *endpointTestContext) RenderJSON(status int, body any) error {
	c.renderStatus = status
	c.renderBody = body
	return nil
}
func (c *endpointTestContext) RenderNoContent(status int) error {
	c.noContentStatus = status
	return nil
}
func (c *endpointTestContext) CurrentService() any { return nil }
func (c *endpointTestContext) CurrentEndpoint() (EndpointType, ResourceVerb, string) {
	return EndpointVerb, ResourceGet, ""
}
func (c *endpointTestContext) Setup(any, EndpointType, ResourceVerb, string) {}

type coreConstraintContext struct {
	element     any
	contentType string
	bind        func(any) error
}

func (c coreConstraintContext) Native() any                             { return nil }
func (c coreConstraintContext) GetPathParam(string) (string, error)     { return "", nil }
func (c coreConstraintContext) GetQueryParam(string) (string, error)    { return "", nil }
func (c coreConstraintContext) GetQueryParams(string) ([]string, error) { return nil, nil }
func (c coreConstraintContext) GetHeader(name string) (string, error) {
	if name == "Content-Type" {
		return c.contentType, nil
	}
	return "", nil
}
func (c coreConstraintContext) GetHeaders(string) ([]string, error) { return nil, nil }
func (c coreConstraintContext) GetCookie(string) (Cookie, error)    { return Cookie{}, nil }
func (c coreConstraintContext) BindJSON(target any) error {
	if c.bind != nil {
		return c.bind(target)
	}
	return nil
}
func (c coreConstraintContext) SetHeader(string, string)    {}
func (c coreConstraintContext) SetCookie(Cookie)            {}
func (c coreConstraintContext) GetData(string) (any, bool)  { return nil, false }
func (c coreConstraintContext) SetData(string, any)         {}
func (c coreConstraintContext) PushElement(any)             {}
func (c coreConstraintContext) PopElement() (any, bool)     { return nil, false }
func (c coreConstraintContext) PeekElement(int) (any, bool) { return c.element, c.element != nil }
func (c coreConstraintContext) RenderJSON(int, any) error   { return nil }
func (c coreConstraintContext) RenderNoContent(int) error   { return nil }
func (c coreConstraintContext) CurrentService() any         { return nil }
func (c coreConstraintContext) CurrentEndpoint() (EndpointType, ResourceVerb, string) {
	return EndpointVerb, ResourceGet, ""
}
func (c coreConstraintContext) Setup(any, EndpointType, ResourceVerb, string) {}
