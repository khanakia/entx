package entgqlmulti

import "entgo.io/ent/entc/gen"

// Naming helpers extracted from gen.Funcs, matching the same conventions used by entgql.
var (
	camel    = gen.Funcs["camel"].(func(string) string)
	pascal   = gen.Funcs["pascal"].(func(string) string)
	plural   = gen.Funcs["plural"].(func(string) string)
	snake    = gen.Funcs["snake"].(func(string) string)
	singular = gen.Funcs["singular"].(func(string) string)
)

// connectionTypeName returns the Connection type name: "ChatbotConnection".
func connectionTypeName(typeName string) string {
	return typeName + "Connection"
}

// edgeTypeName returns the Edge type name: "ChatbotEdge".
func edgeTypeName(typeName string) string {
	return typeName + "Edge"
}

// orderTypeName returns the Order input type name: "ChatbotOrder".
func orderTypeName(typeName string) string {
	return typeName + "Order"
}

// orderFieldEnumName returns the OrderField enum name: "ChatbotOrderField".
func orderFieldEnumName(typeName string) string {
	return typeName + "OrderField"
}

// whereInputName returns the WhereInput type name: "ChatbotWhereInput".
func whereInputName(typeName string) string {
	return typeName + "WhereInput"
}

// queryFieldName derives the query field name from a type name.
// "Chatbot" → "chatbots", "AiModel" → "aiModels".
func queryFieldName(typeName string) string {
	return camel(snake(plural(typeName)))
}

// createInputName returns the Create input type name: "CreateChatbotInput".
func createInputName(typeName string) string {
	return "Create" + typeName + "Input"
}

// updateInputName returns the Update input type name: "UpdateChatbotInput".
func updateInputName(typeName string) string {
	return "Update" + typeName + "Input"
}
