"""Design tokens for Remote DOM templates.

Centralizes typography, spacing, icons, and layout constants to ensure
consistent visual styling across all Remote DOM templates.

These tokens are consumed by Remote DOM templates (notes.py, tasks.py)
and should mirror the styling used in the native ToolBridge Flutter UI.
"""

from typing import Dict, Any


# ═══════════════════════════════════════════════════════════════════════════════
# Typography Tokens
# ═══════════════════════════════════════════════════════════════════════════════

class TextStyle:
    """Text style tokens matching Flutter's Material 3 TextTheme."""
    
    # Display styles (largest)
    DISPLAY_LARGE = "displayLarge"
    DISPLAY_MEDIUM = "displayMedium"
    DISPLAY_SMALL = "displaySmall"
    
    # Headline styles
    HEADLINE_LARGE = "headlineLarge"
    HEADLINE_MEDIUM = "headlineMedium"
    HEADLINE_SMALL = "headlineSmall"
    HEADLINE = "headline"  # Alias for headlineSmall
    
    # Title styles
    TITLE_LARGE = "titleLarge"
    TITLE_MEDIUM = "titleMedium"
    TITLE_SMALL = "titleSmall"
    TITLE = "title"  # Alias for titleMedium
    
    # Body styles
    BODY_LARGE = "bodyLarge"
    BODY_MEDIUM = "bodyMedium"
    BODY_SMALL = "bodySmall"
    BODY = "body"  # Alias for bodyMedium
    
    # Label styles (smallest)
    LABEL_LARGE = "labelLarge"
    LABEL_MEDIUM = "labelMedium"
    LABEL_SMALL = "labelSmall"
    CAPTION = "caption"  # Alias for labelSmall


# ═══════════════════════════════════════════════════════════════════════════════
# Spacing Tokens
# ═══════════════════════════════════════════════════════════════════════════════

class Spacing:
    """Spacing constants for consistent layout."""
    
    # Base unit (4dp)
    UNIT = 4
    
    # Common gaps
    GAP_XS = 4      # Extra small gap
    GAP_SM = 8      # Small gap
    GAP_MD = 12     # Medium gap (default list gap)
    GAP_LG = 16     # Large gap
    GAP_XL = 24     # Extra large gap
    
    # Padding presets
    PADDING_CARD = 16
    PADDING_LIST_ITEM = 16
    PADDING_CONTAINER = 16
    PADDING_CHIP = {"left": 8, "right": 8, "top": 4, "bottom": 4}
    
    # Layout defaults
    LIST_GAP = 12
    CARD_GAP = 12
    SECTION_GAP = 16


# ═══════════════════════════════════════════════════════════════════════════════
# Layout Tokens
# ═══════════════════════════════════════════════════════════════════════════════

class Layout:
    """Layout constraints and defaults."""
    
    # Max widths for different view types
    MAX_WIDTH_LIST = 640
    MAX_WIDTH_DETAIL = 640
    MAX_WIDTH_CARD = 600
    
    # Default chat framing metadata
    CHAT_FRAME_CARD = "card"
    CHAT_FRAME_FLAT = "flat"


# ═══════════════════════════════════════════════════════════════════════════════
# Color Tokens
# ═══════════════════════════════════════════════════════════════════════════════

class Color:
    """Color token names matching Flutter's Material 3 ColorScheme."""
    
    # Primary colors
    PRIMARY = "primary"
    ON_PRIMARY = "onPrimary"
    PRIMARY_CONTAINER = "primaryContainer"
    ON_PRIMARY_CONTAINER = "onPrimaryContainer"
    
    # Secondary colors
    SECONDARY = "secondary"
    ON_SECONDARY = "onSecondary"
    SECONDARY_CONTAINER = "secondaryContainer"
    ON_SECONDARY_CONTAINER = "onSecondaryContainer"
    
    # Surface colors
    SURFACE = "surface"
    ON_SURFACE = "onSurface"
    SURFACE_VARIANT = "surfaceVariant"
    ON_SURFACE_VARIANT = "onSurfaceVariant"
    SURFACE_CONTAINER_LOWEST = "surfaceContainerLowest"
    SURFACE_CONTAINER_LOW = "surfaceContainerLow"
    SURFACE_CONTAINER = "surfaceContainer"
    SURFACE_CONTAINER_HIGH = "surfaceContainerHigh"
    SURFACE_CONTAINER_HIGHEST = "surfaceContainerHighest"
    
    # Error colors
    ERROR = "error"
    ON_ERROR = "onError"
    ERROR_CONTAINER = "errorContainer"
    ON_ERROR_CONTAINER = "onErrorContainer"
    
    # Outline colors
    OUTLINE = "outline"
    OUTLINE_VARIANT = "outlineVariant"


# ═══════════════════════════════════════════════════════════════════════════════
# Icon Tokens
# ═══════════════════════════════════════════════════════════════════════════════

class Icon:
    """Icon names matching Flutter's Material Icons."""
    
    # Notes & Tasks
    NOTES = "notes"
    NOTE = "note"
    NOTE_ADD = "note_add"
    TASK = "task"
    TASK_ALT = "task_alt"
    CHECKLIST = "checklist"
    ASSIGNMENT = "assignment"
    
    # Actions
    VISIBILITY = "visibility"
    DELETE = "delete"
    DELETE_OUTLINE = "delete_outline"
    ARCHIVE = "archive"
    UNARCHIVE = "unarchive"
    EDIT = "edit"
    ADD = "add"
    CHECK = "check"
    CLOSE = "close"
    REFRESH = "refresh"
    
    # Status
    CHECK_CIRCLE = "check_circle"
    PENDING = "pending"
    HOURGLASS = "hourglass_empty"
    ERROR = "error"
    WARNING = "warning"
    
    # Priority & Labels
    PRIORITY_HIGH = "priority_high"
    FLAG = "flag"
    LABEL = "label"
    TAG = "tag"
    BOOKMARK = "bookmark"
    
    # Time
    CALENDAR = "calendar_today"
    SCHEDULE = "schedule"
    ACCESS_TIME = "access_time"
    
    # Misc
    OPEN_IN_NEW = "open_in_new"
    LINK = "link"


# ═══════════════════════════════════════════════════════════════════════════════
# Chip Variants
# ═══════════════════════════════════════════════════════════════════════════════

class ChipVariant:
    """Chip style variants."""
    
    FILLED = "filled"       # Solid background
    OUTLINED = "outlined"   # Border only
    ASSIST = "assist"       # Subtle, informational


# ═══════════════════════════════════════════════════════════════════════════════
# Button Variants
# ═══════════════════════════════════════════════════════════════════════════════

class ButtonVariant:
    """Button style variants."""
    
    PRIMARY = "primary"     # ElevatedButton
    ELEVATED = "elevated"   # ElevatedButton (alias)
    SECONDARY = "secondary" # OutlinedButton
    OUTLINED = "outlined"   # OutlinedButton (alias)
    TEXT = "text"           # TextButton


# ═══════════════════════════════════════════════════════════════════════════════
# Helper Functions
# ═══════════════════════════════════════════════════════════════════════════════

def chip_node(
    label: str,
    variant: str = ChipVariant.ASSIST,
    icon: str | None = None,
) -> Dict[str, Any]:
    """Create a chip node dict.
    
    Args:
        label: Text label for the chip
        variant: Chip style variant (filled, outlined, assist)
        icon: Optional icon name
        
    Returns:
        Remote DOM node dict for a chip
    """
    props: Dict[str, Any] = {
        "label": label,
        "variant": variant,
    }
    if icon:
        props["icon"] = icon
    
    return {
        "type": "chip",
        "props": props,
    }


def wrap_node(
    children: list,
    gap: int = Spacing.GAP_SM,
    run_spacing: int | None = None,
) -> Dict[str, Any]:
    """Create a wrap node dict.
    
    Args:
        children: List of child nodes
        gap: Main axis spacing
        run_spacing: Cross axis spacing (defaults to gap)
        
    Returns:
        Remote DOM node dict for a wrap layout
    """
    return {
        "type": "wrap",
        "props": {
            "gap": gap,
            "runSpacing": run_spacing or gap,
        },
        "children": children,
    }


def text_node(
    text: str,
    style: str = TextStyle.BODY_MEDIUM,
    color: str | None = None,
    max_lines: int | None = None,
    overflow: str | None = None,
) -> Dict[str, Any]:
    """Create a text node dict.
    
    Args:
        text: Text content
        style: Typography style token
        color: Optional color token
        max_lines: Optional max lines
        overflow: Optional overflow mode (ellipsis, fade, clip)
        
    Returns:
        Remote DOM node dict for text
    """
    props: Dict[str, Any] = {
        "text": text,
        "style": style,
    }
    if color:
        props["color"] = color
    if max_lines is not None:
        props["maxLines"] = max_lines
    if overflow:
        props["overflow"] = overflow
    
    return {
        "type": "text",
        "props": props,
    }


def get_chat_metadata(
    frame_style: str = Layout.CHAT_FRAME_FLAT,
    max_width: int | None = None,
) -> Dict[str, Any]:
    """Get chat framing metadata for UI resources.
    
    Args:
        frame_style: Frame style (flat, card)
        max_width: Optional max width constraint
        
    Returns:
        Metadata dict for uiMetadata field
    """
    metadata: Dict[str, Any] = {
        "chat.frameStyle": frame_style,
    }
    if max_width is not None:
        metadata["chat.maxWidth"] = max_width
    return metadata
