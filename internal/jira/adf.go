package jira

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// adfNode is one node of an Atlassian Document.
type adfNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Attrs   map[string]any `json:"attrs"`
	Content []adfNode      `json:"content"`
	Marks   []struct {
		Type  string         `json:"type"`
		Attrs map[string]any `json:"attrs"`
	} `json:"marks"`
}

// maxADFDepth stops a hostile document from nesting forever.
const maxADFDepth = 24

// ADFText turns an Atlassian document (or a plain JSON string, or null) into
// readable text: paragraphs, headings, lists, code, quotes, panels, tables,
// links, mentions and emojis.
func ADFText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var doc adfNode
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	return strings.TrimSpace(strings.Join(blocks(doc.Content, 0), "\n"))
}

// blocks renders sibling blocks, a blank line between those that are not list items.
func blocks(ns []adfNode, depth int) []string {
	var out []string
	for _, n := range ns {
		ls := block(n, depth)
		if len(ls) == 0 {
			continue
		}
		if len(out) > 0 && !tight(n.Type) && !(depth > 0 && isList(n.Type)) {
			out = append(out, "")
		}
		out = append(out, ls...)
	}
	return out
}

func isList(t string) bool {
	return t == "bulletList" || t == "orderedList" || t == "taskList" || t == "decisionList"
}

func tight(t string) bool {
	return t == "listItem" || t == "taskItem" || t == "decisionItem"
}

func block(n adfNode, depth int) []string {
	if depth > maxADFDepth {
		return nil
	}
	switch n.Type {
	case "paragraph":
		return lines(inline(n.Content))
	case "heading":
		level := min(max(attrInt(n.Attrs, "level", 1), 1), 6)
		return []string{strings.Repeat("#", level) + " " + oneLine(inline(n.Content))}
	case "bulletList":
		return list(n, depth, func(int) string { return "• " })
	case "orderedList":
		start := attrInt(n.Attrs, "order", 1)
		return list(n, depth, func(i int) string { return strconv.Itoa(start+i) + ". " })
	case "taskList", "decisionList":
		return list(n, depth, func(int) string { return "• " })
	case "codeBlock":
		return prefixed(lines(inline(n.Content)), "│ ", "│ ")
	case "blockquote":
		return prefixed(blocks(n.Content, depth+1), "> ", "> ")
	case "panel":
		first := "┃ [" + attrStr(n.Attrs, "panelType", "info") + "] "
		return prefixed(blocks(n.Content, depth+1), first, "┃ ")
	case "rule":
		return []string{"────────"}
	case "table":
		return table(n, depth)
	case "expand", "nestedExpand":
		head := []string{"▸ " + attrStr(n.Attrs, "title", "")}
		return append(head, blocks(n.Content, depth+1)...)
	case "mediaSingle", "mediaGroup", "media":
		return []string{"[" + mediaName(n) + "]"}
	case "listItem", "taskItem", "decisionItem":
		return blocks(n.Content, depth+1)
	}
	if len(n.Content) > 0 {
		return blocks(n.Content, depth+1)
	}
	return lines(inline([]adfNode{n}))
}

// list renders the items of a list; nested lists are indented under their item.
func list(n adfNode, depth int, marker func(i int) string) []string {
	var out []string
	for i, it := range n.Content {
		m := marker(i)
		if it.Type == "taskItem" {
			m = "[ ] "
			if attrStr(it.Attrs, "state", "") == "DONE" {
				m = "[x] "
			}
		}
		out = append(out, prefixed(blocks(it.Content, depth+1), m, strings.Repeat(" ", len([]rune(m))))...)
	}
	return out
}

// table is a row a line, cells apart by " │ ", and a rule under a header row.
func table(n adfNode, depth int) []string {
	var out []string
	for _, row := range n.Content {
		var cells []string
		header := false
		for _, c := range row.Content {
			header = header || c.Type == "tableHeader"
			cells = append(cells, oneLine(strings.Join(blocks(c.Content, depth+1), " ")))
		}
		out = append(out, strings.Join(cells, " │ "))
		if header {
			out = append(out, strings.Repeat("─", max(3, len([]rune(out[len(out)-1])))))
		}
	}
	return out
}

func prefixed(ls []string, first, rest string) []string {
	out := make([]string, 0, len(ls))
	for i, l := range ls {
		p := rest
		if i == 0 {
			p = first
		}
		out = append(out, strings.TrimRight(p+l, " "))
	}
	return out
}

// inline renders text and the inline nodes among it.
func inline(ns []adfNode) string {
	var b strings.Builder
	for _, n := range ns {
		switch n.Type {
		case "text":
			b.WriteString(marked(n))
		case "hardBreak":
			b.WriteString("\n")
		case "mention":
			b.WriteString(attrStr(n.Attrs, "text", "@"+attrStr(n.Attrs, "id", "")))
		case "emoji":
			b.WriteString(attrStr(n.Attrs, "text", attrStr(n.Attrs, "shortName", "")))
		case "inlineCard", "blockCard", "embedCard":
			b.WriteString(attrStr(n.Attrs, "url", ""))
		case "status":
			b.WriteString("[" + attrStr(n.Attrs, "text", "") + "]")
		case "date":
			b.WriteString(dateAttr(n.Attrs))
		case "media", "mediaInline":
			b.WriteString("[" + mediaName(n) + "]")
		default:
			if n.Text != "" {
				b.WriteString(n.Text)
			}
			b.WriteString(inline(n.Content))
		}
	}
	return b.String()
}

// marked is a text with its link (the URL after it) and code marks.
func marked(n adfNode) string {
	t := n.Text
	for _, m := range n.Marks {
		switch m.Type {
		case "code":
			t = "`" + t + "`"
		case "link":
			if href := attrStr(m.Attrs, "href", ""); href != "" && href != n.Text {
				t += " (" + href + ")"
			}
		}
	}
	return t
}

func dateAttr(a map[string]any) string {
	ms, err := strconv.ParseInt(attrStr(a, "timestamp", ""), 10, 64)
	if err != nil {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02")
}

func mediaName(n adfNode) string {
	for _, c := range n.Content {
		if c.Type == "media" {
			return mediaName(c)
		}
	}
	if a := attrStr(n.Attrs, "alt", ""); a != "" {
		return "image: " + a
	}
	return "attachment"
}

func lines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r", ""), "\n")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func attrStr(a map[string]any, k, def string) string {
	switch v := a[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return def
}

func attrInt(a map[string]any, k string, def int) int {
	switch v := a[k].(type) {
	case float64:
		return int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
