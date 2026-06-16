package main

import "github.com/ehmo/gum/internal/catalog"

const (
	scopeTasksReadonly = "https://www.googleapis.com/auth/tasks.readonly"
	scopeTasks         = "https://www.googleapis.com/auth/tasks"
)

// BuildTasksOps returns the Tier A Tasks v1 surface: the task-list resource
// (list/get/insert/update/delete) and the task resource (list/get/insert/
// update/delete/move/clear). All use auth_strategy=byo_oauth and
// backend_kind=typed-rest-sdk.
func BuildTasksOps() []catalog.Op {
	op := func(opID, variantID, title, summary string, risk catalog.RiskClass, scopes []string, method, path, goCall string) catalog.Op {
		return makeTasksOp(tasksOpSpec{
			opID: opID, variantID: variantID, title: title, summary: summary,
			riskClass: risk, scopes: scopes, httpMethod: method, httpPath: path, goCall: goCall,
		})
	}
	ro := []string{scopeTasksReadonly}
	rw := []string{scopeTasks}
	const base = "https://www.googleapis.com/tasks/v1"
	return []catalog.Op{
		// --- task lists ---
		op("tasks.tasklists.list", "tasks.v1.rest.tasklists.list", "List Task Lists",
			"List the authenticated user's task lists.",
			catalog.RiskClassRead, ro, "GET", base+"/users/@me/lists", "Tasklists.List"),
		op("tasks.tasklists.get", "tasks.v1.rest.tasklists.get", "Get a Task List",
			"Fetch a task list by id.",
			catalog.RiskClassRead, ro, "GET", base+"/users/@me/lists/{tasklist}", "Tasklists.Get"),
		op("tasks.tasklists.insert", "tasks.v1.rest.tasklists.insert", "Create a Task List",
			"Create a new task list (args.body: title).",
			catalog.RiskClassWrite, rw, "POST", base+"/users/@me/lists", "Tasklists.Insert"),
		op("tasks.tasklists.update", "tasks.v1.rest.tasklists.update", "Update a Task List",
			"Rename a task list (args.body: title).",
			catalog.RiskClassWrite, rw, "PUT", base+"/users/@me/lists/{tasklist}", "Tasklists.Update"),
		op("tasks.tasklists.delete", "tasks.v1.rest.tasklists.delete", "Delete a Task List",
			"Delete a task list and all its tasks. Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, rw, "DELETE", base+"/users/@me/lists/{tasklist}", "Tasklists.Delete"),
		// --- tasks ---
		op("tasks.tasks.list", "tasks.v1.rest.tasks.list", "List Tasks",
			"List tasks in the specified task list.",
			catalog.RiskClassRead, ro, "GET", base+"/lists/{tasklist}/tasks", "Tasks.List"),
		op("tasks.tasks.get", "tasks.v1.rest.tasks.get", "Get a Task",
			"Fetch a single task by id.",
			catalog.RiskClassRead, ro, "GET", base+"/lists/{tasklist}/tasks/{task}", "Tasks.Get"),
		op("tasks.tasks.insert", "tasks.v1.rest.tasks.insert", "Insert a Task",
			"Create a new task on the specified task list (args.body: title, notes, due, etc.).",
			catalog.RiskClassWrite, rw, "POST", base+"/lists/{tasklist}/tasks", "Tasks.Insert"),
		op("tasks.tasks.update", "tasks.v1.rest.tasks.update", "Update a Task",
			"Update a task (mark complete via status=completed, edit title/notes/due).",
			catalog.RiskClassWrite, rw, "PUT", base+"/lists/{tasklist}/tasks/{task}", "Tasks.Update"),
		op("tasks.tasks.delete", "tasks.v1.rest.tasks.delete", "Delete a Task",
			"Delete a task from a task list. Destructive — requires confirmation per §6.1.",
			catalog.RiskClassDestructive, rw, "DELETE", base+"/lists/{tasklist}/tasks/{task}", "Tasks.Delete"),
		op("tasks.tasks.move", "tasks.v1.rest.tasks.move", "Move a Task",
			"Move a task to another position or parent within its list (reorder / nest).",
			catalog.RiskClassWrite, rw, "POST", base+"/lists/{tasklist}/tasks/{task}/move", "Tasks.Move"),
		op("tasks.tasks.clear", "tasks.v1.rest.tasks.clear", "Clear Completed Tasks",
			"Clear all completed tasks from a task list (they become hidden).",
			catalog.RiskClassWrite, rw, "POST", base+"/lists/{tasklist}/clear", "Tasks.Clear"),
	}
}

// tasksOpSpec is the internal shape used to declare a Tasks op.
type tasksOpSpec struct {
	opID       string
	variantID  string
	title      string
	summary    string
	riskClass  catalog.RiskClass
	scopes     []string
	httpMethod string
	httpPath   string
	goCall     string
}

// makeTasksOp builds a catalog.Op for one Tasks v1 operation.
func makeTasksOp(s tasksOpSpec) catalog.Op {
	return catalog.Op{
		OpID:             s.opID,
		OpSchemaVersion:  1,
		Title:            s.title,
		Summary:          s.summary,
		Service:          "tasks",
		ServiceFamily:    "workspace",
		DefaultVariantID: s.variantID,
		Variants: []catalog.Variant{
			{
				VariantID:            s.variantID,
				VariantSchemaVersion: 1,
				Version:              "v1",
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
				BackendKind:          catalog.BackendKindTypedRestSDK,
				Preferred:            true,
				RiskClass:            s.riskClass,
				AuthStrategy:         catalog.AuthStrategyBYOOAuth,
				Scopes:               s.scopes,
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "rest.typed-rest-sdk",
					OperationKey:         s.opID,
					HTTP: &catalog.HTTPBinding{
						Method: s.httpMethod,
						Path:   s.httpPath,
					},
					GoPkg:  "google.golang.org/api/tasks/v1",
					GoCall: s.goCall,
				},
			},
		},
	}
}
