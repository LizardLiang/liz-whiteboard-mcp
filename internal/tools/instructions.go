package tools

// Instructions is the server-level guidance every session receives. Claude Code
// truncates server instructions at 4,096 characters; a test keeps this shorter.
const Instructions = `liz-whiteboard: read and edit ER diagram whiteboards and freeform canvas boards (shapes, text, connectors, tables) in the user's projects.

Toolsets. A client may select tools with the X-Liz-Toolsets request header, a comma list of er, canvas, manage, or all (default, no header). Tools in core are always present.
- core: list_projects, get_project_tree, list_folders, list_whiteboards, list_canvas_boards, get_tool_guide.
- er: ER whiteboard reads and writes (tables, columns, relationships, areas, references, DDL, image).
- canvas: canvas board reads and writes (elements, connectors, tables, image).
- manage: create, rename and delete projects, folders, whiteboards and canvas boards.
If a tool you need is missing, the client's toolset excludes it: tell the user to add that toolset to X-Liz-Toolsets.

Read small before large.
- Call get_schema_summary before get_board, and get_canvas_summary before get_canvas_board. The summaries are compact; the full reads carry UUIDs, styles and props.
- Start from list_projects or get_project_tree to find ids.

Guides. Call get_tool_guide before the first batch_schema_update, create_canvas_table, edit_canvas_table, create_canvas_element, update_canvas_element, create_canvas_connector, or create_table_reference. It returns the rules the short tool descriptions leave out.

Deletes. Deleting a project, folder, whiteboard, canvas board or table reference needs confirmName, the exact current name. A mismatch deletes nothing.`
