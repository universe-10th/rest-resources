package gorm

import (
	"errors"
	"reflect"

	gormtypes "github.com/universe-10th/rest-resources/gorm/types"
	"github.com/universe-10th/rest-resources/types"
	gormdb "gorm.io/gorm"
)

// Storage stores resources in a GORM-backed table. The DB handle is supplied by
// the caller and may already include connection pooling, transactions, or scopes.
type Storage[IDT comparable, RT types.Resource[IDT]] struct {
	db               *gormdb.DB
	mapping          *types.FieldsMapping
	filterSerializer gormtypes.FilterSerializer
	filterValidator  gormtypes.FilterValidator
	sortSerializer   gormtypes.SortSerializer
	sortValidator    gormtypes.SortValidator
}

// NewStorage creates a GORM storage for RT using db as the backing connection.
func NewStorage[IDT comparable, RT types.Resource[IDT]](db *gormdb.DB) *Storage[IDT, RT] {
	mapping := gormtypes.NewFieldsMapping[IDT, RT]()
	return &Storage[IDT, RT]{
		db:               db,
		mapping:          mapping,
		filterSerializer: gormtypes.NewFilterSerializer(mapping),
		filterValidator:  gormtypes.NewFilterValidator(mapping),
		sortSerializer:   gormtypes.NewSortSerializer(mapping),
		sortValidator:    gormtypes.NewSortValidator(mapping),
	}
}

func (s *Storage[IDT, RT]) Mapping() *types.FieldsMapping {
	return s.mapping
}

func (s *Storage[IDT, RT]) GetElement(filter *types.FilterExpression) (RT, bool, error) {
	element, target := newElement[RT]()
	err := s.applyFilter(s.model(), filter).First(target).Error
	if errors.Is(err, gormdb.ErrRecordNotFound) {
		var zero RT
		return zero, false, nil
	}
	if err != nil {
		var zero RT
		return zero, false, err
	}

	return element, true, nil
}

func (s *Storage[IDT, RT]) GetElements(
	filter *types.FilterExpression,
	sort *types.SortExpression,
	skip int64,
	limit int64,
) ([]RT, int64, error) {
	var total int64
	if err := s.applyFilter(s.model(), filter).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query := s.applyFilter(s.model(), filter)
	if sort != nil {
		if order := s.sortSerializer.Serialize(*sort); order != "" {
			query = query.Order(order)
		}
	}
	if skip > 0 {
		query = query.Offset(int(skip))
	}
	if limit > 0 {
		query = query.Limit(int(limit))
	}

	elements, target := newElementSlice[RT]()
	if err := query.Find(target).Error; err != nil {
		return nil, 0, err
	}

	return elements, total, nil
}

func (s *Storage[IDT, RT]) Save(element RT) (bool, error) {
	if isNilElement(element) {
		return true, nil
	}

	if isZero(element.GetID()) {
		element.SetCreationTime()
		element.SetLastUpdateTime()
		return false, s.db.Create(derefElement(element)).Error
	}

	existing, found, err := s.getByID(element.GetID(), false)
	if err != nil || !found {
		return !found, err
	}

	element.RestoreCreationTime(existing.GetCreationTime())
	element.SetLastUpdateTime()
	return false, s.db.Save(derefElement(element)).Error
}

func (s *Storage[IDT, RT]) Delete(element RT) (bool, error) {
	if isNilElement(element) || isZero(element.GetID()) {
		return true, nil
	}

	existing, found, err := s.getByID(element.GetID(), false)
	if err != nil || !found {
		return !found, err
	}

	if softDeleted, ok := any(existing).(types.SoftDeletedResource[IDT]); ok {
		softDeleted.SetDeletionTime()
		softDeleted.SetLastUpdateTime()
		return false, s.db.Save(derefElement(existing)).Error
	}

	result := s.db.Delete(derefElement(existing))
	return result.RowsAffected == 0, result.Error
}

func (s *Storage[IDT, RT]) ValidateFilter(filter *types.FilterExpression) error {
	return validateFilterExpression(filter, s.filterValidator)
}

func (s *Storage[IDT, RT]) ValidateSort(sort *types.SortExpression) error {
	return validateSortExpression(sort, s.sortValidator)
}

func (s *Storage[IDT, RT]) AddIDFilter(filter *types.FilterExpression, id IDT) {
	filter.Restrict(&types.FilterExpression{
		Operator: types.FilterEQ,
		Field:    s.idJSONField(),
		Value:    id,
	})
}

func (s *Storage[IDT, RT]) Restore(element RT) (bool, error) {
	if isNilElement(element) || isZero(element.GetID()) {
		return true, nil
	}

	existing, found, err := s.getByID(element.GetID(), true)
	if err != nil || !found {
		return !found, err
	}

	softDeleted, ok := any(existing).(types.SoftDeletedResource[IDT])
	if !ok {
		return true, nil
	}

	softDeleted.UnsetDeletionTime()
	softDeleted.SetLastUpdateTime()
	err = s.db.Unscoped().Save(derefElement(existing)).Error
	if err == nil {
		copyElement(element, existing)
	}
	return false, err
}

func (s *Storage[IDT, RT]) Prune(element RT) (bool, error) {
	if isNilElement(element) || isZero(element.GetID()) {
		return true, nil
	}

	existing, found, err := s.getByID(element.GetID(), true)
	if err != nil || !found {
		return !found, err
	}

	result := s.db.Unscoped().Delete(derefElement(existing))
	return result.RowsAffected == 0, result.Error
}

func (s *Storage[IDT, RT]) AddDeletedFilter(filter *types.FilterExpression, deleted bool) {
	resource := sampleElement[RT]()
	softDeleted, ok := any(resource).(types.SoftDeletedResource[IDT])
	if !ok {
		if deleted {
			filter.Restrict(&types.FilterExpression{Operator: types.FilterNone})
		}
		return
	}

	filter.Restrict(&types.FilterExpression{
		Operator: types.FilterNull,
		Field:    softDeleted.GetDeletionTimeField(),
		Value:    !deleted,
	})
}

func (s *Storage[IDT, RT]) model() *gormdb.DB {
	return s.db.Model(newModel[RT]())
}

func (s *Storage[IDT, RT]) applyFilter(query *gormdb.DB, filter *types.FilterExpression) *gormdb.DB {
	if filter == nil || filter.Operator == "" {
		return query
	}

	return query.Where(s.filterSerializer.Serialize(*filter))
}

func (s *Storage[IDT, RT]) getByID(id IDT, deleted bool) (RT, bool, error) {
	filter := types.FilterExpression{}
	s.AddIDFilter(&filter, id)
	s.AddDeletedFilter(&filter, deleted)

	query := s.model()
	if deleted {
		query = query.Unscoped()
	}

	element, target := newElement[RT]()
	err := s.applyFilter(query, &filter).First(target).Error
	if errors.Is(err, gormdb.ErrRecordNotFound) {
		var zero RT
		return zero, false, nil
	}
	if err != nil {
		var zero RT
		return zero, false, err
	}

	return element, true, nil
}

func (s *Storage[IDT, RT]) idJSONField() string {
	resource := sampleElement[RT]()
	if field := resource.GetIDField(); field != "" {
		return field
	}
	if field := s.mapping.FieldToJSON("ID"); field != "" {
		return field
	}
	return "id"
}

func newModel[RT any]() any {
	valueType := reflect.TypeOf((*RT)(nil)).Elem()
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return reflect.New(valueType).Interface()
}

func newElement[RT any]() (RT, any) {
	var zero RT
	valueType := reflect.TypeOf(zero)
	if valueType == nil {
		valueType = reflect.TypeOf((*RT)(nil)).Elem()
	}

	if valueType.Kind() == reflect.Pointer {
		elementValue := reflect.New(valueType.Elem())
		return elementValue.Interface().(RT), elementValue.Interface()
	}

	target := reflect.New(valueType)
	return target.Elem().Interface().(RT), target.Interface()
}

func sampleElement[RT any]() RT {
	element, _ := newElement[RT]()
	return element
}

func newElementSlice[RT any]() ([]RT, any) {
	elements := []RT{}
	return elements, &elements
}

func derefElement[RT any](element RT) any {
	return element
}

func copyElement[RT any](target RT, source RT) {
	targetValue := reflect.ValueOf(target)
	sourceValue := reflect.ValueOf(source)
	if !targetValue.IsValid() || !sourceValue.IsValid() ||
		targetValue.Kind() != reflect.Pointer || sourceValue.Kind() != reflect.Pointer ||
		targetValue.IsNil() || sourceValue.IsNil() {
		return
	}

	targetValue.Elem().Set(sourceValue.Elem())
}

func isNilElement[RT any](element RT) bool {
	value := reflect.ValueOf(element)
	return !value.IsValid() || (value.Kind() == reflect.Pointer && value.IsNil())
}

func isZero[T comparable](value T) bool {
	var zero T
	return value == zero
}
