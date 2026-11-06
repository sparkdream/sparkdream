// Package contentguard holds the test helpers behind the "labelled bodies are
// withheld" guard (docs/content-scanning.md §3.4): every query that returns a
// content record must blank the body of a media-flagged record, and only the
// per-module body query may return it. Each content module's keeper tests
// walk their Query service with QueriesReturning, so a new query that returns
// a content record fails the guard until the test exercises it.
package contentguard

import (
	"reflect"
	"sort"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/registry"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// QueriesReturning lists, sorted, the methods of the named gRPC service whose
// response message holds one of the content messages (full proto names) at
// any depth.
func QueriesReturning(t testing.TB, service string, contents ...string) []string {
	t.Helper()
	desc, err := registry.MergedProtoRegistry().FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		t.Fatalf("service %s not found: %v", service, err)
	}
	svc, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		t.Fatalf("%s is not a service: %T", service, desc)
	}
	want := map[protoreflect.FullName]bool{}
	for _, c := range contents {
		want[protoreflect.FullName(c)] = true
	}
	var out []string
	methods := svc.Methods()
	for i := 0; i < methods.Len(); i++ {
		m := methods.Get(i)
		if holds(m.Output(), want, map[protoreflect.FullName]bool{}) {
			out = append(out, string(m.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func holds(md protoreflect.MessageDescriptor, want, seen map[protoreflect.FullName]bool) bool {
	if want[md.FullName()] {
		return true
	}
	if seen[md.FullName()] {
		return false
	}
	seen[md.FullName()] = true
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		if f.IsMap() {
			f = f.MapValue()
		}
		if f.Message() != nil && holds(f.Message(), want, seen) {
			return true
		}
	}
	return false
}

// Withheld walks resp (a query response) for every value of type T, fails the
// test when flagged(v) holds and body(v) is not empty, and returns how many
// flagged records it saw, so a caller can require that the query actually
// returned the record under test.
func Withheld[T any](t testing.TB, resp any, flagged func(T) bool, body func(T) string) int {
	t.Helper()
	n := 0
	walk(reflect.ValueOf(resp), reflect.TypeOf((*T)(nil)).Elem(), func(v reflect.Value) {
		rec := v.Interface().(T)
		if !flagged(rec) {
			return
		}
		n++
		if b := body(rec); b != "" {
			t.Errorf("%T: flagged record served with its body (%d bytes)", resp, len(b))
		}
	}, 0)
	return n
}

func walk(v reflect.Value, target reflect.Type, visit func(reflect.Value), depth int) {
	if !v.IsValid() || depth > 16 {
		return
	}
	if v.Type() == target {
		visit(v)
		return
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			walk(v.Elem(), target, visit, depth+1)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				walk(v.Field(i), target, visit, depth+1)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			walk(v.Index(i), target, visit, depth+1)
		}
	case reflect.Map:
		it := v.MapRange()
		for it.Next() {
			walk(it.Value(), target, visit, depth+1)
		}
	}
}
