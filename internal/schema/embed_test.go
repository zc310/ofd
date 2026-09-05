package schema

import (
	"io"
	"path"
	"testing"

	"github.com/knroy/go-xml/xsd"
)

func TestDefaultCompilesAllRootSchemas(t *testing.T) {
	set, err := Default()
	if err != nil {
		t.Fatalf("compile embedded schemas: %v", err)
	}
	for root := range roots {
		if schema, ok := set.Schema(root); !ok || schema == nil {
			t.Errorf("missing compiled schema for %s", root)
		}
	}
}

func TestCatalogResolvesIncludedSchemas(t *testing.T) {
	resolver := xsd.NewCatalogResolver()
	for _, filename := range []string{"Definitions.xsd", "Page.xsd"} {
		source, err := Files.ReadFile(path.Join("xsd", filename))
		if err != nil {
			t.Fatal(err)
		}
		resolver.Add(namespace, source, filename, "ofd://schema/"+filename)
	}
	reader, resolved, err := resolver.Resolve("", "Definitions.xsd", "ofd://schema/Page.xsd")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "ofd://schema/Definitions.xsd" {
		t.Fatalf("resolved name = %q", resolved)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("resolved included schema is empty")
	}
}

func TestPageSchemaCompilesWithCatalog(t *testing.T) {
	resolver := xsd.NewCatalogResolver()
	for _, filename := range []string{"Definitions.xsd", "Page.xsd"} {
		source, err := Files.ReadFile(path.Join("xsd", filename))
		if err != nil {
			t.Fatal(err)
		}
		resolver.Add(namespace, source, filename, "ofd://schema/"+filename)
	}
	if _, err := xsd.LoadFile("Page.xsd", xsd.Options{Resolver: resolver}); err != nil {
		t.Fatalf("compile Page.xsd: %v", err)
	}
}

func TestExtensionsSchemaCompilesWithAllCatalogEntries(t *testing.T) {
	resolver := xsd.NewCatalogResolver()
	definitions, err := Files.ReadFile(path.Join("xsd", "Definitions.xsd"))
	if err != nil {
		t.Fatal(err)
	}
	resolver.Add(namespace, definitions, "Definitions.xsd", "ofd://schema/Definitions.xsd")
	for _, filename := range roots {
		source, err := Files.ReadFile(path.Join("xsd", filename))
		if err != nil {
			t.Fatal(err)
		}
		resolver.Add(namespace, source, filename, "ofd://schema/"+filename)
	}
	reader, resolved, err := resolver.Resolve("", "Definitions.xsd", "ofd://schema/Extensions.xsd")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "ofd://schema/Definitions.xsd" || len(data) == 0 {
		t.Fatalf("Definitions.xsd resolved as %q with %d bytes", resolved, len(data))
	}
	if _, err := xsd.LoadFile("Extensions.xsd", xsd.Options{Resolver: resolver}); err != nil {
		t.Fatalf("compile Extensions.xsd: %v", err)
	}
}
