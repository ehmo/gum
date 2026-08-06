---
title: Field masks
description: "Use Google partial-response fields to keep gum API calls small."
---

# Field masks

Many Google REST APIs accept a `fields` parameter. It asks Google to return only
the response fields needed by the caller.

Use it whenever an agent or script needs a small, predictable payload:

```bash
gum read drive.files.list \
  --args '{"pageSize":10,"fields":"files(id,name,mimeType),nextPageToken"}' \
  --output json
```

Field masks run upstream at Google. That makes them different from local output
formatting: omitted fields are never downloaded by gum for that request.

## Pattern

1. Run `gum describe <op_id>` to check the operation and example arguments.
2. Start with the smallest field list that can answer the task.
3. Keep pagination tokens when listing resources.
4. Use `--output json` when another program will parse the result.

## Notes

- Field names come from the Google API response schema, not from gum-specific
  aliases.
- A bad `fields` expression is rejected by the upstream API.
- Some operations do not support partial response. For those, use gum output
  shaping after the full response is returned.
