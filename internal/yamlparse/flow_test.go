package yamlparse

import "testing"

func TestFlowCollections(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"mixed seq", `v: [a, "b", 3]`,
			`{"v": ["a"@1:5, "b"q@1:8, "3"@1:13]@1:4}@1:1`},
		{"quoted methods", `methods: ["GET", "POST"]`,
			`{"methods": ["GET"q@1:11, "POST"q@1:18]@1:10}@1:1`},
		{"simple map", `v: { k: v }`,
			`{"v": {"k": "v"@1:9}@1:4}@1:1`},
		{"empty nested", `v: { add: {}, remove: [] }`,
			`{"v": {"add": {}@1:11, "remove": []@1:23}@1:4}@1:1`},
		{"nested", `v: {a: [1, {b: "x"}], c: {d: [[]]}}`,
			`{"v": {"a": ["1"@1:9, {"b": "x"q@1:16}@1:12]@1:8, "c": {"d": [[]@1:31]@1:30}@1:26}@1:4}@1:1`},
		{"no spaces", `v: [1,2,3]`,
			`{"v": ["1"@1:5, "2"@1:7, "3"@1:9]@1:4}@1:1`},
		{"trailing comma", `v: [a, b, ]`,
			`{"v": ["a"@1:5, "b"@1:8]@1:4}@1:1`},
		{"quoted keys json style", `v: {"a":1, 'b': "x"}`,
			`{"v": {"a": "1"@1:9, "b": "x"q@1:17}@1:4}@1:1`},
		{"url value", `v: { url: "http://localhost:3003" }`,
			`{"v": {"url": "http://localhost:3003"q@1:11}@1:4}@1:1`},
		{"plain url value", `v: {url: http://h:1/p, w: 3}`,
			`{"v": {"url": "http://h:1/p"@1:10, "w": "3"@1:27}@1:4}@1:1`},
		{"null value", `v: {a: , b: }`,
			`{"v": {"a": null@1:5, "b": null@1:10}@1:4}@1:1`},
		{"null word", `v: [null, ~, "null"]`,
			`{"v": [null@1:5, null@1:11, "null"q@1:14]@1:4}@1:1`},
		{"seq item flow map", "- { url: a }\n- {url: b, weight: 1}\n",
			`[{"url": "a"@1:10}@1:3, {"url": "b"@2:9, "weight": "1"@2:20}@2:3]@1:1`},
		{"root flow", `{gateway: {}}`,
			`{"gateway": {}@1:11}@1:1`},
		{"flow on next line", "v:\n  [a, b]\n",
			`{"v": ["a"@2:4, "b"@2:7]@2:3}@1:1`},
		{"inner spaces", `v: [ a b , c ]`,
			`{"v": ["a b"@1:6, "c"@1:12]@1:4}@1:1`},
		{"hash without space", `v: [a#b]`,
			`{"v": ["a#b"@1:5]@1:4}@1:1`},
		{"spans lines", "v: [a,\n  b]\n",
			`{"v": ["a"@1:5, "b"@2:3]@1:4}@1:1`},
		{"map spans lines", "v: {\n  k: v }\n",
			`{"v": {"k": "v"@2:6}@1:4}@1:1`},
		{"envelope", `result: { payload: "$body", ok: true }`,
			`{"result": {"payload": "$body"q@1:20, "ok": "true"@1:33}@1:9}@1:1`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dump(mustParse(t, c.src), true); got != c.want {
				t.Errorf("ParseYAML(%q)\n got %s\nwant %s", c.src, got, c.want)
			}
		})
	}
}
