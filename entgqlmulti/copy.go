package entgqlmulti

import "github.com/vektah/gqlparser/v2/ast"

// copyDefinition deep-copies an ast.Definition.
// The copy is safe to mutate without affecting the original.
func copyDefinition(d *ast.Definition) *ast.Definition {
	if d == nil {
		return nil
	}
	cp := &ast.Definition{
		Kind:        d.Kind,
		Description: d.Description,
		Name:        d.Name,
		Directives:  copyDirectiveList(d.Directives),
		Fields:      copyFieldList(d.Fields),
		BuiltIn:     d.BuiltIn,
	}
	if len(d.Interfaces) > 0 {
		cp.Interfaces = make([]string, len(d.Interfaces))
		copy(cp.Interfaces, d.Interfaces)
	}
	if len(d.Types) > 0 {
		cp.Types = make([]string, len(d.Types))
		copy(cp.Types, d.Types)
	}
	if len(d.EnumValues) > 0 {
		cp.EnumValues = copyEnumValueList(d.EnumValues)
	}
	return cp
}

// copyFieldList deep-copies a FieldList.
func copyFieldList(fl ast.FieldList) ast.FieldList {
	if fl == nil {
		return nil
	}
	cp := make(ast.FieldList, len(fl))
	for i, f := range fl {
		cp[i] = copyFieldDefinition(f)
	}
	return cp
}

// copyFieldDefinition deep-copies a FieldDefinition.
func copyFieldDefinition(f *ast.FieldDefinition) *ast.FieldDefinition {
	if f == nil {
		return nil
	}
	return &ast.FieldDefinition{
		Description:  f.Description,
		Name:         f.Name,
		Arguments:    copyArgumentDefinitionList(f.Arguments),
		DefaultValue: copyValue(f.DefaultValue),
		Type:         copyType(f.Type),
		Directives:   copyDirectiveList(f.Directives),
	}
}

// copyArgumentDefinitionList deep-copies an ArgumentDefinitionList.
func copyArgumentDefinitionList(al ast.ArgumentDefinitionList) ast.ArgumentDefinitionList {
	if al == nil {
		return nil
	}
	cp := make(ast.ArgumentDefinitionList, len(al))
	for i, a := range al {
		cp[i] = copyArgumentDefinition(a)
	}
	return cp
}

// copyArgumentDefinition deep-copies an ArgumentDefinition.
func copyArgumentDefinition(a *ast.ArgumentDefinition) *ast.ArgumentDefinition {
	if a == nil {
		return nil
	}
	return &ast.ArgumentDefinition{
		Description:  a.Description,
		Name:         a.Name,
		DefaultValue: copyValue(a.DefaultValue),
		Type:         copyType(a.Type),
		Directives:   copyDirectiveList(a.Directives),
	}
}

// copyType deep-copies an ast.Type (recursive for list types).
func copyType(t *ast.Type) *ast.Type {
	if t == nil {
		return nil
	}
	return &ast.Type{
		NamedType: t.NamedType,
		Elem:      copyType(t.Elem),
		NonNull:   t.NonNull,
	}
}

// copyDirectiveList deep-copies a DirectiveList.
func copyDirectiveList(dl ast.DirectiveList) ast.DirectiveList {
	if dl == nil {
		return nil
	}
	cp := make(ast.DirectiveList, len(dl))
	for i, d := range dl {
		cp[i] = copyDirective(d)
	}
	return cp
}

// copyDirective deep-copies a Directive.
func copyDirective(d *ast.Directive) *ast.Directive {
	if d == nil {
		return nil
	}
	return &ast.Directive{
		Name:      d.Name,
		Arguments: copyArgumentList(d.Arguments),
	}
}

// copyArgumentList deep-copies an ArgumentList.
func copyArgumentList(al ast.ArgumentList) ast.ArgumentList {
	if al == nil {
		return nil
	}
	cp := make(ast.ArgumentList, len(al))
	for i, a := range al {
		cp[i] = copyArgument(a)
	}
	return cp
}

// copyArgument deep-copies an Argument.
func copyArgument(a *ast.Argument) *ast.Argument {
	if a == nil {
		return nil
	}
	return &ast.Argument{
		Name:  a.Name,
		Value: copyValue(a.Value),
	}
}

// copyValue deep-copies a Value (recursive for list/object values).
func copyValue(v *ast.Value) *ast.Value {
	if v == nil {
		return nil
	}
	cp := &ast.Value{
		Raw:  v.Raw,
		Kind: v.Kind,
	}
	if len(v.Children) > 0 {
		cp.Children = make(ast.ChildValueList, len(v.Children))
		for i, c := range v.Children {
			cp.Children[i] = &ast.ChildValue{
				Name:  c.Name,
				Value: copyValue(c.Value),
			}
		}
	}
	return cp
}

// copyEnumValueList deep-copies an EnumValueList.
func copyEnumValueList(evl ast.EnumValueList) ast.EnumValueList {
	if evl == nil {
		return nil
	}
	cp := make(ast.EnumValueList, len(evl))
	for i, ev := range evl {
		cp[i] = &ast.EnumValueDefinition{
			Description: ev.Description,
			Name:        ev.Name,
			Directives:  copyDirectiveList(ev.Directives),
		}
	}
	return cp
}

// copyDirectiveDefinition deep-copies an ast.DirectiveDefinition.
func copyDirectiveDefinition(d *ast.DirectiveDefinition) *ast.DirectiveDefinition {
	if d == nil {
		return nil
	}
	cp := &ast.DirectiveDefinition{
		Description:  d.Description,
		Name:         d.Name,
		Arguments:    copyArgumentDefinitionList(d.Arguments),
		IsRepeatable: d.IsRepeatable,
	}
	if len(d.Locations) > 0 {
		cp.Locations = make([]ast.DirectiveLocation, len(d.Locations))
		copy(cp.Locations, d.Locations)
	}
	return cp
}
