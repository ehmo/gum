package embedded_test

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

func TestP2TasksSlidesAdminDepthOps(t *testing.T) {
	cat := loadEmbeddedCatalog(t)
	ops := map[string]catalog.Op{}
	for _, op := range cat.Ops {
		ops[op.OpID] = op
	}

	want := map[string]struct {
		risk   catalog.RiskClass
		fields []string
	}{
		"tasks.tasklists.list":             {catalog.RiskClassRead, []string{"maxResults", "pageToken"}},
		"tasks.tasklists.get":              {catalog.RiskClassRead, []string{"tasklist"}},
		"tasks.tasklists.insert":           {catalog.RiskClassWrite, []string{"title"}},
		"tasks.tasklists.update":           {catalog.RiskClassWrite, []string{"tasklist", "title"}},
		"tasks.tasklists.delete":           {catalog.RiskClassDestructive, []string{"tasklist"}},
		"tasks.tasks.list":                 {catalog.RiskClassRead, []string{"tasklist", "maxResults"}},
		"tasks.tasks.get":                  {catalog.RiskClassRead, []string{"tasklist", "task"}},
		"tasks.tasks.insert":               {catalog.RiskClassWrite, []string{"tasklist", "title"}},
		"tasks.tasks.update":               {catalog.RiskClassWrite, []string{"tasklist", "task", "title"}},
		"tasks.tasks.delete":               {catalog.RiskClassDestructive, []string{"tasklist", "task"}},
		"tasks.tasks.move":                 {catalog.RiskClassWrite, []string{"tasklist", "task"}},
		"tasks.tasks.clear":                {catalog.RiskClassWrite, []string{"tasklist"}},
		"slides.presentations.get":         {catalog.RiskClassRead, []string{"presentationId"}},
		"slides.presentations.create":      {catalog.RiskClassWrite, []string{"title"}},
		"slides.presentations.batchUpdate": {catalog.RiskClassWrite, []string{"presentationId"}},
		"admin.directory.users.list":       {catalog.RiskClassRead, []string{"customer"}},
		"admin.directory.users.get":        {catalog.RiskClassRead, []string{"userKey"}},
		"admin.directory.groups.list":      {catalog.RiskClassRead, []string{"customer"}},
		"admin.directory.groups.get":       {catalog.RiskClassRead, []string{"groupKey"}},
		"admin.directory.members.list":     {catalog.RiskClassRead, []string{"groupKey"}},
		"admin.directory.members.get":      {catalog.RiskClassRead, []string{"groupKey", "memberKey"}},
		"admin.directory.users.insert":     {catalog.RiskClassWrite, []string{"primaryEmail", "name"}},
		"admin.directory.users.update":     {catalog.RiskClassWrite, []string{"userKey"}},
		"admin.directory.users.delete":     {catalog.RiskClassDestructive, []string{"userKey"}},
		"admin.directory.groups.insert":    {catalog.RiskClassWrite, []string{"email"}},
		"admin.directory.groups.update":    {catalog.RiskClassWrite, []string{"groupKey"}},
		"admin.directory.groups.delete":    {catalog.RiskClassDestructive, []string{"groupKey"}},
		"admin.directory.members.insert":   {catalog.RiskClassWrite, []string{"groupKey", "email", "role"}},
		"admin.directory.members.delete":   {catalog.RiskClassDestructive, []string{"groupKey", "memberKey"}},
		"adminreports.activities.list":     {catalog.RiskClassRead, []string{"userKey", "applicationName"}},
		"adminreports.customerUsageReports.get": {catalog.RiskClassRead,
			[]string{"date"}},
		"adminreports.userUsageReport.get": {catalog.RiskClassRead, []string{"userKey", "date"}},
		"adminreports.entityUsageReports.get": {catalog.RiskClassRead,
			[]string{"entityType", "entityKey", "date"}},
	}

	for opID, w := range want {
		t.Run(opID, func(t *testing.T) {
			op, ok := ops[opID]
			if !ok {
				t.Fatalf("generated catalog missing %s", opID)
			}
			v := defaultVariant(&op)
			if v == nil {
				t.Fatalf("%s has no default variant", opID)
			}
			if v.RiskClass != w.risk {
				t.Fatalf("%s risk_class=%q want %q", opID, v.RiskClass, w.risk)
			}
			if len(op.RequestFields) == 0 {
				t.Fatalf("%s has no request_fields", opID)
			}
			for _, field := range w.fields {
				if !requestFieldPresent(op.RequestFields, field) {
					t.Fatalf("%s missing request field %q in %#v", opID, field, op.RequestFields)
				}
			}
		})
	}
}

func requestFieldPresent(fields []catalog.RequestField, name string) bool {
	for _, field := range fields {
		if field.Name == name {
			return true
		}
	}
	return false
}
