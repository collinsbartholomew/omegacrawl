package jsengine

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/tracing"
)

const ExtractAccessibilityTreeScript = `
	(function() {
		function getAccessibilityInfo(node) {
			const info = {
				role: node.getAttribute('role') || '',
				name: node.getAttribute('aria-label') || node.getAttribute('aria-labelledby') || '',
				description: node.getAttribute('aria-describedby') || '',
				checked: node.getAttribute('aria-checked') || '',
				disabled: node.getAttribute('aria-disabled') || '',
				expanded: node.getAttribute('aria-expanded') || '',
				hidden: node.getAttribute('aria-hidden') || '',
				level: node.getAttribute('aria-level') || '',
				live: node.getAttribute('aria-live') || '',
				pressed: node.getAttribute('aria-pressed') || '',
				selected: node.getAttribute('aria-selected') || '',
				controls: node.getAttribute('aria-controls') || '',
				owns: node.getAttribute('aria-owns') || '',
				hasPopup: node.getAttribute('aria-haspopup') || '',
				modal: node.getAttribute('aria-modal') || '',
				orientation: node.getAttribute('aria-orientation') || '',
				placeholder: node.getAttribute('placeholder') || '',
				required: node.getAttribute('aria-required') || '',
				invalid: node.getAttribute('aria-invalid') || '',
				readonly: node.getAttribute('aria-readonly') || '',
				autocomplete: node.getAttribute('autocomplete') || '',
				tabIndex: node.getAttribute('tabindex') || '',
				id: node.id || '',
				tag: node.tagName.toLowerCase(),
				text: node.textContent ? node.textContent.trim().substring(0, 200) : '',
				children: []
			};

			// Recursively process children
			for (let child of node.children) {
				info.children.push(getAccessibilityInfo(child));
			}

			return info;
		}

		// Start from body or document
		const root = document.body || document.documentElement;
		return getAccessibilityInfo(root);
	})()
`

// AccessibilityNode represents a node in the accessibility tree
type AccessibilityNode struct {
	Role        string               `json:"role"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Checked     string               `json:"checked"`
	Disabled    string               `json:"disabled"`
	Expanded    string               `json:"expanded"`
	Hidden      string               `json:"hidden"`
	Level       string               `json:"level"`
	Live        string               `json:"live"`
	Pressed     string               `json:"pressed"`
	Selected    string               `json:"selected"`
	Controls    string               `json:"controls"`
	Owns        string               `json:"owns"`
	HasPopup    string               `json:"hasPopup"`
	Modal       string               `json:"modal"`
	Orientation string               `json:"orientation"`
	Placeholder string               `json:"placeholder"`
	Required    string               `json:"required"`
	Invalid     string               `json:"invalid"`
	ReadOnly    string               `json:"readonly"`
	Autocomplete string              `json:"autocomplete"`
	TabIndex    string               `json:"tabIndex"`
	ID          string               `json:"id"`
	Tag         string               `json:"tag"`
	Text        string               `json:"text"`
	Children    []AccessibilityNode  `json:"children"`
}

// AccessibilityTree represents the full accessibility tree of a page
type AccessibilityTree struct {
	Root       AccessibilityNode `json:"root"`
	Timestamp  string            `json:"timestamp"`
	URL        string            `json:"url"`
	NodeCount  int               `json:"node_count"`
	MaxDepth   int               `json:"max_depth"`
}

// ExtractAccessibilityTree captures the accessibility tree of the current page
func ExtractAccessibilityTree(ctx context.Context) (*AccessibilityTree, error) {
	ctx, span := tracing.StartSpan(ctx, "jsengine.extractAccessibilityTree")
	defer span.End()

	var rawResult map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(ExtractAccessibilityTreeScript, &rawResult))
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	// Convert to structured type
	root := convertToAccessibilityNode(rawResult)
	
	tree := &AccessibilityTree{
		Root:      root,
		Timestamp: time.Now().Format(time.RFC3339),
		NodeCount: countNodes(root),
		MaxDepth:  maxDepth(root, 0),
	}

	span.SetAttributes(
		attribute.Int("node_count", tree.NodeCount),
		attribute.Int("max_depth", tree.MaxDepth),
	)

	return tree, nil
}

// convertToAccessibilityNode converts raw map to AccessibilityNode
func convertToAccessibilityNode(data map[string]interface{}) AccessibilityNode {
	node := AccessibilityNode{}
	
	if v, ok := data["role"].(string); ok {
		node.Role = v
	}
	if v, ok := data["name"].(string); ok {
		node.Name = v
	}
	if v, ok := data["description"].(string); ok {
		node.Description = v
	}
	if v, ok := data["checked"].(string); ok {
		node.Checked = v
	}
	if v, ok := data["disabled"].(string); ok {
		node.Disabled = v
	}
	if v, ok := data["expanded"].(string); ok {
		node.Expanded = v
	}
	if v, ok := data["hidden"].(string); ok {
		node.Hidden = v
	}
	if v, ok := data["level"].(string); ok {
		node.Level = v
	}
	if v, ok := data["live"].(string); ok {
		node.Live = v
	}
	if v, ok := data["pressed"].(string); ok {
		node.Pressed = v
	}
	if v, ok := data["selected"].(string); ok {
		node.Selected = v
	}
	if v, ok := data["controls"].(string); ok {
		node.Controls = v
	}
	if v, ok := data["owns"].(string); ok {
		node.Owns = v
	}
	if v, ok := data["hasPopup"].(string); ok {
		node.HasPopup = v
	}
	if v, ok := data["modal"].(string); ok {
		node.Modal = v
	}
	if v, ok := data["orientation"].(string); ok {
		node.Orientation = v
	}
	if v, ok := data["placeholder"].(string); ok {
		node.Placeholder = v
	}
	if v, ok := data["required"].(string); ok {
		node.Required = v
	}
	if v, ok := data["invalid"].(string); ok {
		node.Invalid = v
	}
	if v, ok := data["readonly"].(string); ok {
		node.ReadOnly = v
	}
	if v, ok := data["autocomplete"].(string); ok {
		node.Autocomplete = v
	}
	if v, ok := data["tabIndex"].(string); ok {
		node.TabIndex = v
	}
	if v, ok := data["id"].(string); ok {
		node.ID = v
	}
	if v, ok := data["tag"].(string); ok {
		node.Tag = v
	}
	if v, ok := data["text"].(string); ok {
		node.Text = v
	}
	
	if children, ok := data["children"].([]interface{}); ok {
		for _, child := range children {
			if childMap, ok := child.(map[string]interface{}); ok {
				node.Children = append(node.Children, convertToAccessibilityNode(childMap))
			}
		}
	}
	
	return node
}

// countNodes counts total nodes in the tree
func countNodes(node AccessibilityNode) int {
	count := 1
	for _, child := range node.Children {
		count += countNodes(child)
	}
	return count
}

// maxDepth calculates maximum depth of the tree
func maxDepth(node AccessibilityNode, currentDepth int) int {
	maxD := currentDepth
	for _, child := range node.Children {
		d := maxDepth(child, currentDepth+1)
		if d > maxD {
			maxD = d
		}
	}
	return maxD
}

// SaveAccessibilityTree saves the accessibility tree to a JSON file
func SaveAccessibilityTree(tree *AccessibilityTree, filepath string) error {
	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath, data, 0644)
}

// ExtractAccessibilitySummary extracts a summary of accessibility issues
func ExtractAccessibilitySummary(tree *AccessibilityTree) AccessibilitySummary {
	summary := AccessibilitySummary{
		TotalNodes: tree.NodeCount,
		MaxDepth:   tree.MaxDepth,
		Issues:     []AccessibilityIssue{},
	}
	
	// Walk the tree and find issues
	walkTree(tree.Root, func(node AccessibilityNode) {
		// Missing labels on interactive elements
		if isInteractiveElement(node) && node.Name == "" && node.Text == "" {
			summary.Issues = append(summary.Issues, AccessibilityIssue{
				Type:        "missing_label",
				Severity:    "error",
				Element:     node.Tag,
				Selector:    getSelector(node),
				Description: "Interactive element missing accessible name",
				WCAG:        "1.1.1, 4.1.2",
			})
		}
		
		// Images without alt text
		if node.Tag == "img" && node.Name == "" {
			summary.Issues = append(summary.Issues, AccessibilityIssue{
				Type:        "missing_alt",
				Severity:    "error",
				Element:     "img",
				Selector:    getSelector(node),
				Description: "Image missing alt text",
				WCAG:        "1.1.1",
			})
		}
		
		// Form inputs without labels
		if isFormInput(node) && node.Name == "" && node.Placeholder == "" {
			summary.Issues = append(summary.Issues, AccessibilityIssue{
				Type:        "missing_form_label",
				Severity:    "error",
				Element:     node.Tag,
				Selector:    getSelector(node),
				Description: "Form input missing label",
				WCAG:        "1.3.1, 3.3.2",
			})
		}
		
		// Headings hierarchy
		if isHeading(node) {
			level := getHeadingLevel(node)
			if level > 0 && level <= 6 {
				summary.Headings = append(summary.Headings, HeadingInfo{
					Level: level,
					Text:  node.Text,
					ID:    node.ID,
				})
			}
		}
		
		// Landmarks
		if isLandmark(node) {
			summary.Landmarks = append(summary.Landmarks, node.Role)
		}
		
		// ARIA issues
		if node.Invalid == "true" && node.Description == "" {
			summary.Issues = append(summary.Issues, AccessibilityIssue{
				Type:        "aria_invalid_no_description",
				Severity:    "warning",
				Element:     node.Tag,
				Selector:    getSelector(node),
				Description: "Element has aria-invalid but no aria-describedby for error message",
				WCAG:        "3.3.1",
			})
		}
		
		// Missing required attribute on required fields
		if node.Required == "true" && node.Tag != "" {
			idSuffix := ""
			if node.ID != "" {
				idSuffix = "#" + node.ID
			}
			summary.RequiredFields = append(summary.RequiredFields, node.Tag+idSuffix)
		}
	})
	
	// Deduplicate landmarks
	seen := make(map[string]bool)
	uniqueLandmarks := []string{}
	for _, l := range summary.Landmarks {
		if !seen[l] {
			seen[l] = true
			uniqueLandmarks = append(uniqueLandmarks, l)
		}
	}
	summary.Landmarks = uniqueLandmarks
	
	return summary
}

type AccessibilitySummary struct {
	TotalNodes    int                  `json:"total_nodes"`
	MaxDepth      int                  `json:"max_depth"`
	Issues        []AccessibilityIssue `json:"issues"`
	Headings      []HeadingInfo        `json:"headings"`
	Landmarks     []string             `json:"landmarks"`
	RequiredFields []string             `json:"required_fields"`
	ErrorCount    int                  `json:"error_count"`
	WarningCount  int                  `json:"warning_count"`
}

type AccessibilityIssue struct {
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	Element     string `json:"element"`
	Selector    string `json:"selector"`
	Description string `json:"description"`
	WCAG        string `json:"wcag"`
}

type HeadingInfo struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	ID    string `json:"id"`
}

func walkTree(node AccessibilityNode, fn func(AccessibilityNode)) {
	fn(node)
	for _, child := range node.Children {
		walkTree(child, fn)
	}
}

func isInteractiveElement(node AccessibilityNode) bool {
	interactiveRoles := []string{"button", "link", "menuitem", "tab", "checkbox", "radio", "switch", "slider", "spinbutton", "textbox", "combobox", "listbox", "menu", "menubar", "tree", "treeitem"}
	for _, r := range interactiveRoles {
		if node.Role == r {
			return true
		}
	}
	interactiveTags := []string{"a", "button", "input", "select", "textarea", "option"}
	for _, t := range interactiveTags {
		if node.Tag == t {
			return true
		}
	}
	return false
}

func isFormInput(node AccessibilityNode) bool {
	formTags := []string{"input", "select", "textarea"}
	for _, t := range formTags {
		if node.Tag == t {
			return true
		}
	}
	return false
}

func isHeading(node AccessibilityNode) bool {
	return node.Role == "heading" || (len(node.Tag) == 2 && node.Tag[0] == 'h' && node.Tag[1] >= '1' && node.Tag[1] <= '6')
}

func getHeadingLevel(node AccessibilityNode) int {
	if node.Level != "" {
		if l, err := strconv.Atoi(node.Level); err == nil {
			return l
		}
	}
	if len(node.Tag) == 2 && node.Tag[0] == 'h' && node.Tag[1] >= '1' && node.Tag[1] <= '6' {
		return int(node.Tag[1] - '0')
	}
	return 0
}

func isLandmark(node AccessibilityNode) bool {
	landmarkRoles := []string{"main", "navigation", "search", "banner", "contentinfo", "complementary", "form", "region", "article", "aside"}
	for _, r := range landmarkRoles {
		if node.Role == r {
			return true
		}
	}
	return false
}

func getSelector(node AccessibilityNode) string {
	if node.ID != "" {
		return "#" + node.ID
	}
	if node.Role != "" {
		return "[role=" + node.Role + "]"
	}
	return node.Tag
}