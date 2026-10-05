package memory

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/universe-10th/rest-resources/types"
)

// IDGenerator returns the next ID for an inserted element.
type IDGenerator[IDT comparable] func() IDT

// Storage stores resources in process memory. It is intended for tests,
// prototyping, and ephemeral deployments.
type Storage[IDT comparable, RT types.Resource[IDT]] struct {
	mu        sync.RWMutex
	elements  map[IDT]RT
	mapping   *types.FieldsMapping
	generator IDGenerator[IDT]
	nextIntID int64
}

// NewStorage creates an empty memory storage for RT.
func NewStorage[IDT comparable, RT types.Resource[IDT]]() *Storage[IDT, RT] {
	return NewStorageWithIDGenerator[IDT, RT](nil)
}

// NewStorageWithIDGenerator creates an empty memory storage with a custom ID generator.
func NewStorageWithIDGenerator[IDT comparable, RT types.Resource[IDT]](
	generator IDGenerator[IDT],
) *Storage[IDT, RT] {
	return &Storage[IDT, RT]{
		elements:  map[IDT]RT{},
		mapping:   types.NewFieldsMapping[IDT, RT](FieldToStorage),
		generator: generator,
	}
}

func (s *Storage[IDT, RT]) Mapping() *types.FieldsMapping {
	return s.mapping
}

func (s *Storage[IDT, RT]) GetElement(filter *types.FilterExpression) (RT, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, element := range s.elements {
		matches, err := s.matchesFilter(element, filter)
		if err != nil {
			var zero RT
			return zero, false, err
		}
		if matches {
			return cloneElement(element), true, nil
		}
	}

	var zero RT
	return zero, false, nil
}

func (s *Storage[IDT, RT]) GetElements(
	filter *types.FilterExpression,
	sortExpression *types.SortExpression,
	skip int64,
	limit int64,
) ([]RT, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	elements := []RT{}
	for _, element := range s.elements {
		matches, err := s.matchesFilter(element, filter)
		if err != nil {
			return nil, 0, err
		}
		if matches {
			elements = append(elements, cloneElement(element))
		}
	}

	if sortExpression != nil && len(sortExpression.Sort) > 0 {
		sort.SliceStable(elements, func(left int, right int) bool {
			before, _ := s.less(elements[left], elements[right], sortExpression.Sort)
			return before
		})
	}

	total := int64(len(elements))
	if skip > total {
		return []RT{}, total, nil
	}

	start := skip
	end := total
	if limit > 0 && start+limit < end {
		end = start + limit
	}

	return elements[start:end], total, nil
}

func (s *Storage[IDT, RT]) Save(element RT) (bool, error) {
	if isNilElement(element) {
		return true, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	id := element.GetID()
	if isZero(id) {
		id = s.nextID()
		element.SetID(id)
		element.SetCreationTime()
		element.SetLastUpdateTime()
		s.elements[id] = cloneElement(element)
		return false, nil
	}

	existing, ok := s.elements[id]
	if !ok || isDeleted(existing) {
		return true, nil
	}

	element.RestoreCreationTime(existing.GetCreationTime())
	element.SetLastUpdateTime()
	s.elements[id] = cloneElement(element)
	return false, nil
}

func (s *Storage[IDT, RT]) Delete(element RT) (bool, error) {
	if isNilElement(element) || isZero(element.GetID()) {
		return true, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.elements[element.GetID()]
	if !ok || isDeleted(existing) {
		return true, nil
	}

	if softDeleted, ok := any(existing).(types.SoftDeletedResource[IDT]); ok {
		softDeleted.SetDeletionTime()
		softDeleted.SetLastUpdateTime()
		s.elements[existing.GetID()] = cloneElement(softDeleted.(RT))
		copyElement(element, softDeleted.(RT))
		return false, nil
	}

	delete(s.elements, existing.GetID())
	return false, nil
}

func (s *Storage[IDT, RT]) ValidateFilter(filter *types.FilterExpression) error {
	return validateFilterExpression(filter, fieldValidator{mapping: s.mapping})
}

func (s *Storage[IDT, RT]) ValidateSort(sortExpression *types.SortExpression) error {
	if sortExpression == nil {
		return nil
	}

	validator := fieldValidator{mapping: s.mapping}
	for _, item := range sortExpression.Sort {
		if !validator.IsSortable(item.Field, item.Order) {
			return fmt.Errorf("sort field %q is not allowed", item.Field)
		}
	}
	return nil
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

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.elements[element.GetID()]
	if !ok {
		return true, nil
	}

	softDeleted, ok := any(existing).(types.SoftDeletedResource[IDT])
	if !ok || !softDeleted.IsDeleted() {
		return true, nil
	}

	softDeleted.UnsetDeletionTime()
	softDeleted.SetLastUpdateTime()
	s.elements[existing.GetID()] = cloneElement(softDeleted.(RT))
	copyElement(element, softDeleted.(RT))
	return false, nil
}

func (s *Storage[IDT, RT]) Prune(element RT) (bool, error) {
	if isNilElement(element) || isZero(element.GetID()) {
		return true, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.elements[element.GetID()]
	if !ok || !isDeleted(existing) {
		return true, nil
	}

	delete(s.elements, existing.GetID())
	return false, nil
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

func (s *Storage[IDT, RT]) nextID() IDT {
	if s.generator != nil {
		return s.generator()
	}

	var zero IDT
	switch any(zero).(type) {
	case int:
		s.nextIntID++
		return any(int(s.nextIntID)).(IDT)
	case int8:
		s.nextIntID++
		return any(int8(s.nextIntID)).(IDT)
	case int16:
		s.nextIntID++
		return any(int16(s.nextIntID)).(IDT)
	case int32:
		s.nextIntID++
		return any(int32(s.nextIntID)).(IDT)
	case int64:
		s.nextIntID++
		return any(s.nextIntID).(IDT)
	case uint:
		s.nextIntID++
		return any(uint(s.nextIntID)).(IDT)
	case uint8:
		s.nextIntID++
		return any(uint8(s.nextIntID)).(IDT)
	case uint16:
		s.nextIntID++
		return any(uint16(s.nextIntID)).(IDT)
	case uint32:
		s.nextIntID++
		return any(uint32(s.nextIntID)).(IDT)
	case uint64:
		s.nextIntID++
		return any(uint64(s.nextIntID)).(IDT)
	case string:
		return any(uuid.NewString()).(IDT)
	default:
		panic(fmt.Sprintf("memory storage cannot generate IDs of type %T; use NewStorageWithIDGenerator", zero))
	}
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

func (s *Storage[IDT, RT]) matchesFilter(element RT, filter *types.FilterExpression) (bool, error) {
	if filter == nil || filter.Operator == "" {
		return true, nil
	}

	switch filter.Operator {
	case types.FilterNone:
		return false, nil
	case types.FilterAnd:
		for _, expression := range filter.Expressions {
			matches, err := s.matchesFilter(element, &expression)
			if err != nil || !matches {
				return matches, err
			}
		}
		return true, nil
	case types.FilterOr:
		for _, expression := range filter.Expressions {
			matches, err := s.matchesFilter(element, &expression)
			if err != nil {
				return false, err
			}
			if matches {
				return true, nil
			}
		}
		return false, nil
	case types.FilterNot:
		if len(filter.Expressions) != 1 {
			return false, fmt.Errorf("not filter expects one expression")
		}
		matches, err := s.matchesFilter(element, &filter.Expressions[0])
		return !matches, err
	case types.FilterLT, types.FilterLTE, types.FilterGT, types.FilterGTE, types.FilterEQ, types.FilterNE:
		return s.compareField(element, *filter)
	case types.FilterNull:
		value, ok := fieldValueForJSON(s.mapping, element, filter.Field)
		if !ok {
			return false, fmt.Errorf("field %q is not mapped", filter.Field)
		}
		wantNull, _ := filter.Value.(bool)
		return isNullValue(value) == wantNull, nil
	case types.FilterExists:
		_, ok := fieldValueForJSON(s.mapping, element, filter.Field)
		wantExists, _ := filter.Value.(bool)
		return ok == wantExists, nil
	case types.FilterContains:
		value, ok := fieldValueForJSON(s.mapping, element, filter.Field)
		if !ok {
			return false, fmt.Errorf("field %q is not mapped", filter.Field)
		}
		return stringValue(value) != "" && contains(stringValue(value), fmt.Sprint(filter.Value)), nil
	default:
		return false, fmt.Errorf("unsupported filter operator %q", filter.Operator)
	}
}

func (s *Storage[IDT, RT]) compareField(element RT, filter types.FilterExpression) (bool, error) {
	value, ok := fieldValueForJSON(s.mapping, element, filter.Field)
	if !ok {
		return false, fmt.Errorf("field %q is not mapped", filter.Field)
	}

	cmp, ok := compareValues(value.Interface(), filter.Value)
	if !ok {
		return false, fmt.Errorf("field %q cannot be compared to %T", filter.Field, filter.Value)
	}

	switch filter.Operator {
	case types.FilterLT:
		return cmp < 0, nil
	case types.FilterLTE:
		return cmp <= 0, nil
	case types.FilterGT:
		return cmp > 0, nil
	case types.FilterGTE:
		return cmp >= 0, nil
	case types.FilterEQ:
		return cmp == 0, nil
	case types.FilterNE:
		return cmp != 0, nil
	default:
		return false, nil
	}
}

func (s *Storage[IDT, RT]) less(left RT, right RT, sorts []types.Sort) (bool, bool) {
	for _, item := range sorts {
		leftValue, leftOK := fieldValueForJSON(s.mapping, left, item.Field)
		rightValue, rightOK := fieldValueForJSON(s.mapping, right, item.Field)
		if !leftOK || !rightOK {
			continue
		}

		cmp, ok := compareValues(leftValue.Interface(), rightValue.Interface())
		if !ok || cmp == 0 {
			continue
		}
		if item.Order == types.Desc {
			return cmp > 0, true
		}
		return cmp < 0, true
	}

	return false, false
}

// FieldToStorage maps exported struct field names to themselves for memory storage.
func FieldToStorage(v any) map[string]string {
	mapping := map[string]string{}
	valueType := indirectType(reflect.TypeOf(v))
	if valueType == nil || valueType.Kind() != reflect.Struct {
		return mapping
	}

	collectFieldToStorage(valueType, mapping)
	return mapping
}

func collectFieldToStorage(valueType reflect.Type, mapping map[string]string) {
	for i := range valueType.NumField() {
		field := valueType.Field(i)
		if field.PkgPath != "" {
			continue
		}

		fieldType := indirectType(field.Type)
		if field.Anonymous && fieldType != nil && fieldType.Kind() == reflect.Struct {
			collectFieldToStorage(fieldType, mapping)
			continue
		}

		mapping[field.Name] = field.Name
	}
}

func sampleElement[RT any]() RT {
	element, _ := newElement[RT]()
	return element
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

func cloneElement[RT any](element RT) RT {
	value := reflect.ValueOf(element)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() {
		return element
	}

	clone := reflect.New(value.Elem().Type())
	clone.Elem().Set(value.Elem())
	return clone.Interface().(RT)
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

func isDeleted[IDT comparable, RT types.Resource[IDT]](element RT) bool {
	softDeleted, ok := any(element).(types.SoftDeletedResource[IDT])
	return ok && softDeleted.IsDeleted()
}

func fieldValueForJSON(mapping *types.FieldsMapping, element any, jsonField string) (reflect.Value, bool) {
	fieldName := types.FieldForJSON(mapping, jsonField)
	if fieldName == "" {
		return reflect.Value{}, false
	}

	value := reflect.ValueOf(element)
	for value.IsValid() && value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}, false
		}
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}

	field := value.FieldByName(fieldName)
	if !field.IsValid() {
		return reflect.Value{}, false
	}
	return field, true
}

func isNullValue(value reflect.Value) bool {
	for value.IsValid() && value.Kind() == reflect.Interface {
		if value.IsNil() {
			return true
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func compareValues(left any, right any) (int, bool) {
	left = indirectInterface(left)
	right = indirectInterface(right)

	if leftTime, ok := toTime(left); ok {
		rightTime, ok := toTime(right)
		if !ok {
			return 0, false
		}
		return compareTime(leftTime, rightTime), true
	}

	if leftNumber, ok := types.NumericValue(left); ok {
		rightNumber, ok := types.NumericValue(right)
		if !ok {
			return 0, false
		}
		return compareOrdered(leftNumber, rightNumber), true
	}

	switch typedLeft := left.(type) {
	case string:
		typedRight, ok := right.(string)
		if !ok {
			typedRight = fmt.Sprint(right)
		}
		return compareOrdered(typedLeft, typedRight), true
	case bool:
		typedRight, ok := right.(bool)
		if !ok {
			return 0, false
		}
		if typedLeft == typedRight {
			return 0, true
		}
		if !typedLeft {
			return -1, true
		}
		return 1, true
	case nil:
		if right == nil {
			return 0, true
		}
		return -1, true
	default:
		if reflect.TypeOf(left) != nil && reflect.TypeOf(left).Comparable() && left == right {
			return 0, true
		}
		return 0, false
	}
}

func indirectInterface(value any) any {
	if value == nil {
		return nil
	}

	reflected := reflect.ValueOf(value)
	for reflected.IsValid() && (reflected.Kind() == reflect.Pointer || reflected.Kind() == reflect.Interface) {
		if reflected.IsNil() {
			return nil
		}
		reflected = reflected.Elem()
	}
	if !reflected.IsValid() {
		return nil
	}
	return reflected.Interface()
}

func toTime(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, true
	case string:
		parsed, err := time.Parse(time.RFC3339, typed)
		return parsed, err == nil
	default:
		return time.Time{}, false
	}
}

func compareOrdered[T ~float64 | ~string](left T, right T) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func compareTime(left time.Time, right time.Time) int {
	if left.Before(right) {
		return -1
	}
	if left.After(right) {
		return 1
	}
	return 0
}

func stringValue(value reflect.Value) string {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.String {
		return ""
	}
	return value.String()
}

func contains(value string, fragment string) bool {
	if fragment == "" {
		return true
	}
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

func indirectType(valueType reflect.Type) reflect.Type {
	for valueType != nil && valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return valueType
}

type fieldValidator struct {
	mapping *types.FieldsMapping
}

func (v fieldValidator) IsValidCmpFilter(field string, value any) bool {
	structField, ok := types.StructFieldForJSON(v.mapping, field)
	if !ok {
		return false
	}
	return acceptsValue(structField.Type, value)
}

func (v fieldValidator) IsNullCheckable(field string) bool {
	structField, ok := types.StructFieldForJSON(v.mapping, field)
	if !ok {
		return false
	}
	return isNullable(structField.Type)
}

func (v fieldValidator) IsExistenceCheckable(field string) bool {
	_, ok := types.StructFieldForJSON(v.mapping, field)
	return ok
}

func (v fieldValidator) IsContainsCheckable(field string) bool {
	structField, ok := types.StructFieldForJSON(v.mapping, field)
	if !ok {
		return false
	}
	return dereferenceType(structField.Type).Kind() == reflect.String
}

func (v fieldValidator) IsSortable(field string, orderType types.OrderType) bool {
	if orderType != types.Asc && orderType != types.Desc {
		return false
	}

	structField, ok := types.StructFieldForJSON(v.mapping, field)
	return ok && isScalar(structField.Type)
}

func acceptsValue(valueType reflect.Type, value any) bool {
	if value == nil {
		return isNullable(valueType)
	}

	valueType = dereferenceType(valueType)
	if valueType == reflect.TypeOf(time.Time{}) {
		_, ok := toTime(value)
		return ok
	}

	if valueType.Kind() == reflect.String {
		_, ok := value.(string)
		return ok
	}

	if valueType.Kind() == reflect.Bool {
		_, ok := value.(bool)
		return ok
	}

	if isNumericKind(valueType.Kind()) {
		return acceptsNumeric(valueType.Kind(), value)
	}

	return false
}

func acceptsNumeric(kind reflect.Kind, value any) bool {
	number, ok := types.NumericValue(value)
	if !ok {
		if stringValue, ok := value.(string); ok {
			parsed, err := strconv.ParseFloat(stringValue, 64)
			if err != nil {
				return false
			}
			number = parsed
			ok = true
		}
	}
	if !ok {
		return false
	}

	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return number == float64(int64(number))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return number >= 0 && number == float64(uint64(number))
	case reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func isNullable(valueType reflect.Type) bool {
	if valueType == nil {
		return false
	}
	switch valueType.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
		return true
	default:
		return false
	}
}

func isScalar(valueType reflect.Type) bool {
	valueType = dereferenceType(valueType)
	if valueType == nil || valueType == reflect.TypeOf(time.Time{}) {
		return valueType != nil
	}
	switch valueType.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func isNumericKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func dereferenceType(valueType reflect.Type) reflect.Type {
	for valueType != nil && valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return valueType
}

func validateFilterExpression(filter *types.FilterExpression, validator types.FilterValidator) error {
	if filter == nil || filter.Operator == "" {
		return nil
	}

	switch filter.Operator {
	case types.FilterNone:
		return nil
	case types.FilterAnd, types.FilterOr:
		for _, expression := range filter.Expressions {
			if err := validateFilterExpression(&expression, validator); err != nil {
				return err
			}
		}
		return nil
	case types.FilterNot:
		if len(filter.Expressions) != 1 {
			return fmt.Errorf("not filter expects one expression")
		}
		return validateFilterExpression(&filter.Expressions[0], validator)
	case types.FilterLT, types.FilterLTE, types.FilterGT, types.FilterGTE, types.FilterEQ, types.FilterNE:
		if !validator.IsValidCmpFilter(filter.Field, filter.Value) {
			return fmt.Errorf("filter field %q is not allowed", filter.Field)
		}
	case types.FilterNull:
		if !validator.IsNullCheckable(filter.Field) {
			return fmt.Errorf("filter field %q is not null-checkable", filter.Field)
		}
	case types.FilterExists:
		if !validator.IsExistenceCheckable(filter.Field) {
			return fmt.Errorf("filter field %q is not existence-checkable", filter.Field)
		}
	case types.FilterContains:
		if !validator.IsContainsCheckable(filter.Field) {
			return fmt.Errorf("filter field %q is not contains-checkable", filter.Field)
		}
	default:
		return fmt.Errorf("unsupported filter operator %q", filter.Operator)
	}

	return nil
}
