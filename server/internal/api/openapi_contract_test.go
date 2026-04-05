package api

import "testing"

func TestWorkspaceScopedUserRoutesAreCanonical(t *testing.T) {
	doc, err := GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger() error = %v", err)
	}

	if doc.Paths == nil {
		t.Fatal("GetSwagger() returned nil paths")
	}

	if doc.Paths.Value("/users") != nil {
		t.Fatal("public contract still exposes top-level /users")
	}
	if doc.Paths.Value("/users/{id}") != nil {
		t.Fatal("public contract still exposes top-level /users/{id}")
	}
	if doc.Paths.Value("/users/{id}/roles") != nil {
		t.Fatal("public contract still exposes top-level /users/{id}/roles")
	}

	required := []string{
		"/workspaces/{id}/users",
		"/workspaces/{id}/users/{user_id}",
		"/workspaces/{id}/users/{user_id}/roles",
	}
	for _, path := range required {
		item := doc.Paths.Value(path)
		if item == nil {
			t.Fatalf("public contract missing canonical workspace-scoped route %s", path)
		}
		for _, op := range item.Operations() {
			if len(op.Parameters) == 0 {
				t.Fatalf("workspace-scoped user route %s is missing path parameters", path)
			}
		}
	}
}

func TestAuthMeContractIsAccountFirst(t *testing.T) {
	doc, err := GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger() error = %v", err)
	}

	if doc.Paths == nil || doc.Paths.Value("/auth/me") == nil {
		t.Fatal("public contract missing /auth/me")
	}

	schemaRef := doc.Components.Schemas["AuthMeResponse"]
	if schemaRef == nil || schemaRef.Value == nil {
		t.Fatal("public contract missing AuthMeResponse schema")
	}

	for _, property := range []string{"account_id", "user_id", "account", "user"} {
		if _, ok := schemaRef.Value.Properties[property]; !ok {
			t.Fatalf("AuthMeResponse missing account-first property %q", property)
		}
	}

	if _, ok := schemaRef.Value.Properties["membership_id"]; ok {
		t.Fatal("AuthMeResponse still exposes removed membership_id")
	}
}

func TestAccountContractIsAuthOnly(t *testing.T) {
	doc, err := GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger() error = %v", err)
	}

	schemaRef := doc.Components.Schemas["Account"]
	if schemaRef == nil || schemaRef.Value == nil {
		t.Fatal("public contract missing Account schema")
	}

	for _, property := range []string{"id", "principal_type", "email", "is_bot", "deleted", "created_at", "updated_at"} {
		if _, ok := schemaRef.Value.Properties[property]; !ok {
			t.Fatalf("Account missing canonical auth property %q", property)
		}
	}

	for _, property := range []string{"name", "real_name", "display_name", "profile"} {
		if _, ok := schemaRef.Value.Properties[property]; ok {
			t.Fatalf("Account still exposes workspace persona field %q", property)
		}
	}
}

func TestAuthSchemasDoNotExposeRemovedMembershipFields(t *testing.T) {
	doc, err := GetSwagger()
	if err != nil {
		t.Fatalf("GetSwagger() error = %v", err)
	}

	for _, tc := range []struct {
		schema string
		field  string
	}{
		{schema: "AuthSession", field: "membership_id"},
		{schema: "AuthMeResponse", field: "membership_id"},
		{schema: "WorkspaceInvite", field: "accepted_by_membership_id"},
	} {
		schemaRef := doc.Components.Schemas[tc.schema]
		if schemaRef == nil || schemaRef.Value == nil {
			t.Fatalf("public contract missing %s schema", tc.schema)
		}
		if _, ok := schemaRef.Value.Properties[tc.field]; ok {
			t.Fatalf("%s still exposes removed field %q", tc.schema, tc.field)
		}
	}
}
