package controllers

import (
	"app/base/database"
	"github.com/stretchr/testify/assert"
	"testing"
)

var testFilters = []string{
	"eq:abc",
	"in:a,b,c",
	"gt:13",
	"lt:12",
	"between:12,13",
}

func dummyParser(v string) (interface{}, error) {
	return v, nil
}

func TestFilterParse(t *testing.T) {
	operators := []string{
		"eq", "in", "gt", "lt", "between",
	}

	values := [][]string{
		{"abc"},
		{"a", "b", "c"},
		{"13"},
		{"12"},
		{"12", "13"},
	}

	for i, f := range testFilters {
		filter, err := ParseFilterValue(f)
		assert.Equal(t, nil, err)
		assert.Equal(t, operators[i], filter.Operator)
		assert.Equal(t, values[i], filter.Values)
	}
}

// nolint: govet
func TestFilterToSql(t *testing.T) {
	queries := []string{
		"test = ? ",
		"test IN (?) ",
		"test > ? ",
		"test < ? ",
		"test BETWEEN ? AND ? ",
	}

	for i, f := range testFilters {
		filter, err := ParseFilterValue(f)
		assert.Equal(t, nil, err)

		attrMap := database.AttrMap{"test": {"test", dummyParser, false}}
		query, _, err := filter.ToWhere("test", attrMap)
		assert.Equal(t, nil, err)
		assert.Equal(t, queries[i], query)
	}
}

// nolint: govet
func TestFilterToSqlAdvanced(t *testing.T) {
	queries := []string{
		"(NOT test) = ? ",
		"(NOT test) IN (?) ",
		"(NOT test) > ? ",
		"(NOT test) < ? ",
		"(NOT test) BETWEEN ? AND ? ",
	}

	for i, f := range testFilters {
		filter, err := ParseFilterValue(f)
		assert.Equal(t, nil, err)
		attrMap := database.AttrMap{"test": {"(NOT test)", dummyParser, false}}
		query, _, err := filter.ToWhere("test", attrMap)
		assert.Equal(t, nil, err)
		assert.Equal(t, queries[i], query)
	}
}

// Filter out null characters
func TestFilterInvalidValue(t *testing.T) {
	filter, err := ParseFilterValue("eq:aa\u0000aa")
	assert.NoError(t, err)
	attrMap, _, err := database.GetQueryAttrs(struct{ V string }{""})
	assert.NoError(t, err)
	_, value, err := filter.ToWhere("v", attrMap)
	assert.NoError(t, err)
	assert.Equal(t, []interface{}{"aaaa"}, value)
}
