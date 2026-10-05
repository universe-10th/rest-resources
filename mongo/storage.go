package mongo

import (
	"context"
	"errors"
	"reflect"

	mongotypes "github.com/universe-10th/rest-resources/mongo/types"
	"github.com/universe-10th/rest-resources/types"
	"go.mongodb.org/mongo-driver/v2/bson"
	drivermongo "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Storage stores resources in a MongoDB collection. The collection is supplied
// by the caller so connection ownership stays outside this package.
type Storage[IDT comparable, RT types.Resource[IDT]] struct {
	collection       *drivermongo.Collection
	context          func() context.Context
	mapping          *types.FieldsMapping
	filterSerializer mongotypes.FilterSerializer
	filterValidator  mongotypes.FilterValidator
	sortSerializer   mongotypes.SortSerializer
	sortValidator    mongotypes.SortValidator
}

// NewStorage creates a MongoDB storage for RT using collection as backing table.
func NewStorage[IDT comparable, RT types.Resource[IDT]](collection *drivermongo.Collection) *Storage[IDT, RT] {
	return NewStorageWithContext[IDT, RT](collection, context.Background)
}

// NewStorageWithContext creates a MongoDB storage using contextProvider for each operation.
func NewStorageWithContext[IDT comparable, RT types.Resource[IDT]](
	collection *drivermongo.Collection,
	contextProvider func() context.Context,
) *Storage[IDT, RT] {
	if contextProvider == nil {
		contextProvider = context.Background
	}

	mapping := mongotypes.NewFieldsMapping[IDT, RT]()
	return &Storage[IDT, RT]{
		collection:       collection,
		context:          contextProvider,
		mapping:          mapping,
		filterSerializer: mongotypes.NewFilterSerializer(mapping),
		filterValidator:  mongotypes.NewFilterValidator(mapping),
		sortSerializer:   mongotypes.NewSortSerializer(mapping),
		sortValidator:    mongotypes.NewSortValidator(mapping),
	}
}

func (s *Storage[IDT, RT]) Mapping() *types.FieldsMapping {
	return s.mapping
}

func (s *Storage[IDT, RT]) GetElement(filter *types.FilterExpression) (RT, bool, error) {
	element, target := newElement[RT]()
	err := s.collection.FindOne(s.context(), s.serializeFilter(filter)).Decode(target)
	if errors.Is(err, drivermongo.ErrNoDocuments) {
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
	query := s.serializeFilter(filter)
	total, err := s.collection.CountDocuments(s.context(), query)
	if err != nil {
		return nil, 0, err
	}

	findOptions := options.Find()
	if sort != nil {
		if serialized := s.sortSerializer.Serialize(*sort); len(serialized) > 0 {
			findOptions.SetSort(serialized)
		}
	}
	if skip > 0 {
		findOptions.SetSkip(skip)
	}
	if limit > 0 {
		findOptions.SetLimit(limit)
	}

	cursor, err := s.collection.Find(s.context(), query, findOptions)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(s.context())

	elements := []RT{}
	for cursor.Next(s.context()) {
		element, target := newElement[RT]()
		if err = cursor.Decode(target); err != nil {
			return nil, 0, err
		}
		elements = append(elements, element)
	}
	if err = cursor.Err(); err != nil {
		return nil, 0, err
	}

	return elements, total, nil
}

func (s *Storage[IDT, RT]) Save(element RT) (bool, error) {
	if isNilElement(element) {
		return true, nil
	}

	if isZero(element.GetID()) {
		setGeneratedObjectID(element)
		element.SetCreationTime()
		element.SetLastUpdateTime()
		_, err := s.collection.InsertOne(s.context(), derefElement(element))
		return false, err
	}

	existing, found, err := s.getByID(element.GetID(), false)
	if err != nil || !found {
		return !found, err
	}

	element.RestoreCreationTime(existing.GetCreationTime())
	element.SetLastUpdateTime()
	result, err := s.collection.ReplaceOne(s.context(), s.idFilter(element.GetID()), derefElement(element))
	return result == nil || result.MatchedCount == 0, err
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
		result, err := s.collection.ReplaceOne(s.context(), s.idFilter(existing.GetID()), derefElement(existing))
		return result == nil || result.MatchedCount == 0, err
	}

	result, err := s.collection.DeleteOne(s.context(), s.idFilter(existing.GetID()))
	return result == nil || result.DeletedCount == 0, err
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
	result, err := s.collection.ReplaceOne(s.context(), s.idFilter(existing.GetID()), derefElement(existing))
	if err == nil {
		copyElement(element, existing)
	}
	return result == nil || result.MatchedCount == 0, err
}

func (s *Storage[IDT, RT]) Prune(element RT) (bool, error) {
	if isNilElement(element) || isZero(element.GetID()) {
		return true, nil
	}

	existing, found, err := s.getByID(element.GetID(), true)
	if err != nil || !found {
		return !found, err
	}

	result, err := s.collection.DeleteOne(s.context(), s.idFilter(existing.GetID()))
	return result == nil || result.DeletedCount == 0, err
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

func (s *Storage[IDT, RT]) getByID(id IDT, deleted bool) (RT, bool, error) {
	filter := types.FilterExpression{}
	s.AddIDFilter(&filter, id)
	s.AddDeletedFilter(&filter, deleted)
	return s.GetElement(&filter)
}

func (s *Storage[IDT, RT]) serializeFilter(filter *types.FilterExpression) bson.M {
	if filter == nil || filter.Operator == "" {
		return bson.M{}
	}

	return s.filterSerializer.Serialize(*filter)
}

func (s *Storage[IDT, RT]) idFilter(id IDT) bson.M {
	storageField := types.StorageForJSON(s.mapping, s.idJSONField())
	if storageField == "" {
		storageField = "_id"
	}
	return bson.M{storageField: id}
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

func setGeneratedObjectID[IDT comparable, RT types.Resource[IDT]](element RT) {
	id, ok := any(element.GetID()).(bson.ObjectID)
	if !ok || !id.IsZero() {
		return
	}

	generated := any(bson.NewObjectID()).(IDT)
	element.SetID(generated)
}
