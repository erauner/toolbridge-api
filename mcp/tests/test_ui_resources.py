"""
Unit tests for MCP-UI resource helpers.

Tests the build_ui_with_text function and UIContent type.
"""

import pytest
from mcp.types import TextContent


class TestBuildUIWithText:
    """Test suite for the build_ui_with_text helper."""

    def test_returns_list_with_two_elements(self):
        """Test that the function returns exactly two content blocks."""
        from toolbridge_mcp.ui.resources import build_ui_with_text

        result = build_ui_with_text(
            uri="ui://test/example",
            html="<p>Hello</p>",
            text_summary="Test summary",
        )

        assert isinstance(result, list)
        assert len(result) == 2

    def test_first_element_is_text_content(self):
        """Test that the first element is TextContent with the summary."""
        from toolbridge_mcp.ui.resources import build_ui_with_text

        result = build_ui_with_text(
            uri="ui://test/example",
            html="<p>Hello</p>",
            text_summary="This is a test summary",
        )

        first_element = result[0]
        assert isinstance(first_element, TextContent)
        assert first_element.type == "text"
        assert first_element.text == "This is a test summary"

    def test_second_element_is_ui_resource(self):
        """Test that the second element is a UIResource (EmbeddedResource)."""
        from toolbridge_mcp.ui.resources import build_ui_with_text

        result = build_ui_with_text(
            uri="ui://toolbridge/notes/list",
            html="<ul><li>Note 1</li></ul>",
            text_summary="Test",
        )

        second_element = result[1]
        # UIResource from mcp-ui-server should be an EmbeddedResource
        assert hasattr(second_element, "type")
        assert second_element.type == "resource"

    def test_ui_resource_contains_correct_uri(self):
        """Test that the UIResource has the correct URI."""
        from toolbridge_mcp.ui.resources import build_ui_with_text

        test_uri = "ui://toolbridge/tasks/abc123"
        result = build_ui_with_text(
            uri=test_uri,
            html="<div>Task</div>",
            text_summary="Task summary",
        )

        ui_resource = result[1]
        # URI may be an AnyUrl type, convert to string for comparison
        assert str(ui_resource.resource.uri) == test_uri

    def test_ui_resource_contains_html_content(self):
        """Test that the UIResource contains the HTML content."""
        from toolbridge_mcp.ui.resources import build_ui_with_text

        test_html = "<html><body><h1>Test</h1></body></html>"
        result = build_ui_with_text(
            uri="ui://test/example",
            html=test_html,
            text_summary="Summary",
        )

        ui_resource = result[1]
        # The HTML should be in the resource text field
        assert ui_resource.resource.text == test_html

    def test_ui_resource_has_html_mime_type(self):
        """Test that the UIResource has text/html MIME type."""
        from toolbridge_mcp.ui.resources import build_ui_with_text

        result = build_ui_with_text(
            uri="ui://test/example",
            html="<p>Content</p>",
            text_summary="Summary",
        )

        ui_resource = result[1]
        assert ui_resource.resource.mimeType == "text/html"

    def test_handles_empty_html_raises_error(self):
        """Test that empty HTML string raises an error (mcp-ui-server requirement)."""
        from toolbridge_mcp.ui.resources import build_ui_with_text
        from mcp_ui_server.exceptions import InvalidContentError

        # mcp-ui-server requires non-empty HTML for rawHtml content type
        with pytest.raises(InvalidContentError):
            build_ui_with_text(
                uri="ui://test/empty",
                html="",
                text_summary="Empty content",
            )

    def test_handles_multiline_html(self):
        """Test handling of multiline HTML content."""
        from toolbridge_mcp.ui.resources import build_ui_with_text

        multiline_html = """
        <html>
            <head><title>Test</title></head>
            <body>
                <h1>Hello</h1>
                <p>World</p>
            </body>
        </html>
        """

        result = build_ui_with_text(
            uri="ui://test/multiline",
            html=multiline_html,
            text_summary="Multiline test",
        )

        assert len(result) == 2
        assert "<h1>Hello</h1>" in result[1].resource.text

    def test_handles_special_characters_in_uri(self):
        """Test handling of UIDs with hyphens in URI."""
        from toolbridge_mcp.ui.resources import build_ui_with_text

        result = build_ui_with_text(
            uri="ui://toolbridge/notes/a1b2c3d4-e5f6-7890-abcd-ef1234567890",
            html="<p>Note</p>",
            text_summary="Note with UUID",
        )

        # URI may be an AnyUrl type, convert to string for comparison
        assert "a1b2c3d4-e5f6-7890-abcd-ef1234567890" in str(result[1].resource.uri)


class TestUIContentType:
    """Test suite for UIContent type alias."""

    def test_uicontent_is_list_type(self):
        """Test that UIContent accepts list values."""
        from toolbridge_mcp.ui.resources import UIContent

        # This is a type alias, so we just verify it exists and is usable
        # The actual type checking happens at runtime/mypy level
        assert UIContent is not None
