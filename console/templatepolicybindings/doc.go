// Package templatepolicybindings provides storage for TemplatePolicyBinding
// ConfigMaps. A binding attaches a single TemplatePolicy to an explicit list
// of project templates and/or deployments, replacing the glob-based target
// selector on TemplatePolicyRule (ADR 029, HOL-590). Like TemplatePolicy,
// bindings live only in folder or organization namespaces — never in project
// namespaces (HOL-554).
package templatepolicybindings
