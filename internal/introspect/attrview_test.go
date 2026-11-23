package introspect

import (
	"slices"
	"testing"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

func TestAttrViewNestsOnlyWhenDeclared(t *testing.T) {
	plain := attrView(&schema.ResolvedAttribute{Name: "title", BaseType: "text"})
	want := []string{"name", "type", "required", "enum", "pattern", "default"}
	if !slices.Equal(plain.Keys(), want) {
		t.Errorf("plain keys = %v, want %v", plain.Keys(), want)
	}

	fields := schema.NewOrdered[*schema.ResolvedAttribute]()
	fields.Set("by", &schema.ResolvedAttribute{Name: "by", BaseType: "text", Required: true})
	item := &schema.ResolvedAttribute{Name: "verified", BaseType: "object", Fields: fields}
	list := attrView(&schema.ResolvedAttribute{Name: "verified", BaseType: "list", Items: item})
	if got := list.Keys(); !slices.Equal(got, append(want, "items")) {
		t.Fatalf("list keys = %v", got)
	}
	iv, _ := list.Get("items")
	items := iv.(*omap.Map)
	if _, has := items.Get("name"); has {
		t.Error("an element is anonymous; items must carry no name")
	}
	fv, _ := items.Get("fields")
	by := fv.([]any)[0].(*omap.Map)
	if name, _ := by.Get("name"); name != "by" {
		t.Errorf("items.fields[0].name = %v, want by", name)
	}
}
