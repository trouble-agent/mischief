package catalog

// StatusUnknownPrimitive marks a fault row whose primitive id resolved to
// no descriptor in the loaded catalog (DF-01): the plan row is a typed
// refusal, never an empty-fields row, so scripted consumers can branch on
// it instead of parsing notes lines.
const StatusUnknownPrimitive Status = "unknown_primitive"
