# Classroom, Forms, Meet, and Apps Script

Enable each API you plan to call. Add the matching scopes to the OAuth consent
screen.

Services and scopes:

- Classroom: `classroom.courses.readonly`, `classroom.courses`, `classroom.rosters.readonly`, `classroom.coursework.students.readonly`, `classroom.coursework.students`, `classroom.announcements.readonly`
- Forms: `forms.body.readonly`, `forms.body`, `forms.responses.readonly`
- Meet: `meetings.space.readonly`, `meetings.space.created`
- Apps Script: `script.projects.readonly`, `script.projects`, `script.deployments`

Setup:

```shell
gum login --service classroom,forms,meet,script
gum read classroom.courses.list --args '{"pageSize":5}'
gum read forms.forms.get --args '{"formId":"<form-id>"}'
gum read meet.conferenceRecords.list --args '{"pageSize":5}'
gum read script.projects.get --args '{"scriptId":"<script-id>"}'
```

These APIs often depend on product state. Empty result sets can be correct when
the account has no courses, forms, meetings, or Apps Script projects.
