package ui_test

import (
	"encoding/json"
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/automation"
)

func ExampleTester_Inspect() {
	on := false
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Checkbox(c, &on, "Agree")
	}, 320, 240)
	snapshot := tt.Inspect()
	target, err := snapshot.Resolve(0, "Agree")
	if err != nil {
		panic(err)
	}
	if err := tt.Perform(target, automation.Request{Action: automation.Press}); err != nil {
		panic(err)
	}
	fresh := tt.Inspect()
	for _, node := range fresh.Nodes {
		if node.ID == target.ID {
			fmt.Println("checked:", node.Checked)
		}
	}
	data, err := json.Marshal(fresh)
	if err != nil {
		panic(err)
	}
	fmt.Println("JSON:", json.Valid(data))
	// Output:
	// checked: true
	// JSON: true
}
