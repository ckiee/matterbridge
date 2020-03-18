// Code generated by msgraph-generate.go DO NOT EDIT.

package msgraph

// ContentType undocumented
type ContentType struct {
	// Entity is the base model of ContentType
	Entity
	// Description undocumented
	Description *string `json:"description,omitempty"`
	// Group undocumented
	Group *string `json:"group,omitempty"`
	// Hidden undocumented
	Hidden *bool `json:"hidden,omitempty"`
	// InheritedFrom undocumented
	InheritedFrom *ItemReference `json:"inheritedFrom,omitempty"`
	// Name undocumented
	Name *string `json:"name,omitempty"`
	// Order undocumented
	Order *ContentTypeOrder `json:"order,omitempty"`
	// ParentID undocumented
	ParentID *string `json:"parentId,omitempty"`
	// ReadOnly undocumented
	ReadOnly *bool `json:"readOnly,omitempty"`
	// Sealed undocumented
	Sealed *bool `json:"sealed,omitempty"`
	// ColumnLinks undocumented
	ColumnLinks []ColumnLink `json:"columnLinks,omitempty"`
}