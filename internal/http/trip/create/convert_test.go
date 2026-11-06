package create

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	api "github.com/betharch/TripGo/internal/generated"
)

func TestRequestFieldsMatchContract(t *testing.T) {
	assert.Equal(t, jsonNames(reflect.TypeFor[api.TripData]()), jsonNames(reflect.TypeFor[request]()))
	assert.Equal(t, jsonNames(reflect.TypeFor[api.Coordinates]()), jsonNames(reflect.TypeFor[point]()))
}

func jsonNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
