package jira

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestADFTextRendersTheCommonNodes(t *testing.T) {
	doc := `{"type":"doc","version":1,"content":[
	 {"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Goal"}]},
	 {"type":"paragraph","content":[{"type":"text","text":"Ask "},{"type":"mention","attrs":{"id":"u1","text":"@Ada"}},
	   {"type":"text","text":" see "},{"type":"text","text":"the docs","marks":[{"type":"link","attrs":{"href":"https://x.io/d"}}]},
	   {"type":"text","text":" "},{"type":"emoji","attrs":{"shortName":":tada:","text":"🎉"}},{"type":"hardBreak"},{"type":"text","text":"run "},
	   {"type":"text","text":"make","marks":[{"type":"code"}]}]},
	 {"type":"bulletList","content":[
	   {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]},
	     {"type":"orderedList","attrs":{"order":3},"content":[
	       {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"deep"}]}]},
	       {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"deeper"}]}]}]}]},
	   {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]}]}]},
	 {"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"a := 1\nb := 2"}]},
	 {"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted"}]}]},
	 {"type":"panel","attrs":{"panelType":"warning"},"content":[{"type":"paragraph","content":[{"type":"text","text":"careful"}]}]},
	 {"type":"table","content":[
	   {"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Name"}]}]},{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Qty"}]}]}]},
	   {"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"nuts"}]}]},{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"3"}]}]}]}]},
	 {"type":"rule"},
	 {"type":"mediaSingle","content":[{"type":"media","attrs":{"alt":"shot.png"}}]}
	]}`
	got := ADFText(json.RawMessage(doc))
	for _, want := range []string{
		"## Goal",
		"Ask @Ada see the docs (https://x.io/d) 🎉\nrun `make`",
		"• one\n  3. deep\n  4. deeper\n• two",
		"│ a := 1\n│ b := 2",
		"> quoted",
		"┃ [warning] careful",
		"Name │ Qty\n",
		"nuts │ 3",
		"────────",
		"[image: shot.png]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestADFTextEdgeCases(t *testing.T) {
	if got := ADFText(json.RawMessage(`null`)); got != "" {
		t.Errorf("null: %q", got)
	}
	if got := ADFText(json.RawMessage(`"  plain  "`)); got != "plain" {
		t.Errorf("string: %q", got)
	}
	if got := ADFText(json.RawMessage(`{broken`)); got != "" {
		t.Errorf("broken: %q", got)
	}
	link := `{"type":"text","text":"https://a.io","marks":[{"type":"link","attrs":{"href":"https://a.io"}}]}`
	if got := ADFText(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[` + link + `]}]}`)); got != "https://a.io" {
		t.Errorf("a link that is its own text is not repeated: %q", got)
	}
	deep := `{"type":"text","text":"x"}`
	for i := 0; i < 200; i++ {
		deep = `{"type":"blockquote","content":[` + deep + `]}`
	}
	_ = ADFText(json.RawMessage(`{"type":"doc","content":[` + deep + `]}`)) // must not blow the stack
}
