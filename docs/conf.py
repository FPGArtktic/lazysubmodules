# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

"""Sphinx configuration of the LazySubmodules documentation site.

The site is built from the Markdown pages in this directory with MyST-Parser
and the Furo theme. Read the Docs builds it through ../.readthedocs.yaml with
warnings turned into errors; see project/documentation.md for the local build.
"""

import os

# -- Project ----------------------------------------------------------------

project = "LazySubmodules"
author = "Mateusz Okulanis"
copyright = "2026 Mateusz Okulanis"  # noqa: A001
language = "en"

# -- General ----------------------------------------------------------------

extensions = [
    "myst_parser",
    "sphinx_design",
    "sphinx_copybutton",
    "sphinxext.opengraph",
]

root_doc = "index"
source_suffix = {".md": "markdown"}

exclude_patterns = [
    "_build",
    # The demo directory holds the recordings and the tapes that produce them.
    # The GIFs the pages use are copied by Sphinx because the pages reference
    # them; demo/README.md is contributor documentation with repository-relative
    # links, so it stays on GitHub and is linked from project/contributing.
    "demo/**",
    # social-preview.png is published by html_extra_path, not as a page.
    "assets/**",
    # Help texts included by the command pages, not pages themselves.
    "reference/cli/_generated/**",
]

nitpicky = True

# -- MyST -------------------------------------------------------------------

myst_enable_extensions = [
    # ::: fences for directives that contain other directives.
    "colon_fence",
    # Definition lists for option and key descriptions.
    "deflist",
    # {.class} attributes on inline spans and on blocks.
    "attrs_inline",
    "attrs_block",
]

# Anchors for the headings up to level 3, so that pages can link to a
# subsection with [text](../guide/update.md#fetch-first).
myst_heading_anchors = 3

# -- HTML -------------------------------------------------------------------

html_theme = "furo"
html_title = "LazySubmodules"
html_static_path = ["_static"]
html_css_files = ["css/lsm.css"]
html_logo = "_static/logo.svg"
html_favicon = "_static/favicon.svg"
html_extra_path = ["assets/social-preview.png"]
html_copy_source = False
html_show_sourcelink = False

# Read the Docs exports the canonical URL of the version being built.
html_baseurl = os.environ.get("READTHEDOCS_CANONICAL_URL", "")

pygments_style = "github-light"
pygments_dark_style = "github-dark"

_GITHUB_ICON = (
    '<svg stroke="currentColor" fill="currentColor" stroke-width="0"'
    ' viewBox="0 0 16 16"><path fill-rule="evenodd" d="M8 0C3.58 0 0 3.58 0'
    " 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38"
    " 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01"
    " 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95"
    " 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18"
    " 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82"
    " 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87"
    " 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01"
    ' 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z"/></svg>'
)

html_theme_options = {
    "sidebar_hide_name": False,
    "top_of_page_buttons": ["view", "edit"],
    "source_repository": "https://github.com/FPGArtktic/lazysubmodules/",
    "source_branch": "main",
    "source_directory": "docs/",
    "footer_icons": [
        {
            "name": "GitHub",
            "url": "https://github.com/FPGArtktic/lazysubmodules",
            "html": _GITHUB_ICON,
            "class": "",
        },
    ],
    # Green for the interface accents, cyan-teal for links: the colors of the
    # logo and of the terminal interface. Every pair reaches WCAG AA.
    "light_css_variables": {
        "color-brand-primary": "#1a7f37",
        "color-brand-content": "#0a6e77",
        "color-brand-visited": "#8250df",
        "color-foreground-primary": "#1f2328",
        "color-foreground-secondary": "#424a53",
        "color-foreground-muted": "#59636e",
        "color-foreground-border": "#8c959f",
        "color-background-primary": "#ffffff",
        "color-background-secondary": "#f6f8fa",
        "color-background-hover": "#eaeef2",
        "color-background-border": "#d0d7de",
        "color-highlighted-background": "#dafbe1",
        "color-inline-code-background": "#eff2f5",
        "color-admonition-title--tip": "#1a7f37",
        "color-admonition-title-background--tip": "rgba(26, 127, 55, 0.12)",
        "color-admonition-title--note": "#0a6e77",
        "color-admonition-title-background--note": "rgba(10, 110, 119, 0.12)",
        "color-admonition-title--important": "#9a6700",
        "color-admonition-title-background--important": "rgba(154, 103, 0, 0.12)",
        "font-stack": (
            'system-ui, -apple-system, "Segoe UI", "Noto Sans", Ubuntu, Cantarell,'
            ' "Helvetica Neue", Arial, sans-serif, "Apple Color Emoji",'
            ' "Segoe UI Emoji"'
        ),
        "font-stack--monospace": (
            'ui-monospace, "JetBrains Mono", "Cascadia Mono", "SFMono-Regular", Menlo,'
            ' "Noto Sans Mono", "DejaVu Sans Mono", Consolas, "Liberation Mono",'
            " monospace"
        ),
    },
    "dark_css_variables": {
        "color-brand-primary": "#56d364",
        "color-brand-content": "#39c5cf",
        "color-brand-visited": "#bc8cff",
        "color-foreground-primary": "#e6edf3",
        "color-foreground-secondary": "#c9d1d9",
        "color-foreground-muted": "#8b949e",
        "color-foreground-border": "#6e7681",
        "color-background-primary": "#0d1117",
        "color-background-secondary": "#0f1722",
        "color-background-hover": "#161b22",
        "color-background-border": "#30363d",
        "color-highlighted-background": "#12261e",
        "color-inline-code-background": "#161b22",
        "color-admonition-title--tip": "#56d364",
        "color-admonition-title-background--tip": "rgba(86, 211, 100, 0.12)",
        "color-admonition-title--note": "#39c5cf",
        "color-admonition-title-background--note": "rgba(57, 197, 207, 0.12)",
        "color-admonition-title--important": "#d29922",
        "color-admonition-title-background--important": "rgba(210, 153, 34, 0.12)",
    },
}

# -- sphinx-copybutton ------------------------------------------------------

# In a console block Pygments marks the prompt with .gp and the output with
# .go; both are left out of the copied text, so the reader gets the commands
# alone. The prompt pattern covers blocks that are not highlighted as console.
copybutton_exclude = ".linenos, .gp, .go"
copybutton_prompt_text = r"\$ "
copybutton_prompt_is_regexp = True

# -- sphinxext-opengraph ----------------------------------------------------

ogp_site_url = os.environ.get(
    "READTHEDOCS_CANONICAL_URL", "https://lazysubmodules.readthedocs.io/en/latest/"
)
ogp_site_name = "LazySubmodules"
ogp_image = "social-preview.png"
ogp_image_alt = (
    "LazySubmodules: track Git submodules by branch, tag, tag pattern or commit"
)
ogp_enable_meta_description = True
# Cards are rendered from assets/social-preview.png; generating one per page
# would need matplotlib and a web font.
ogp_social_cards = {"enable": False}
# The image is 1280x640; without this tag X falls back to the small square
# card. Everything else reads the og: tags above.
ogp_custom_meta_tags = ['<meta name="twitter:card" content="summary_large_image" />']

# -- linkcheck --------------------------------------------------------------

linkcheck_ignore = [
    # Requires a signed-in GitHub account.
    r"https://github\.com/FPGArtktic/lazysubmodules/security/advisories/new.*",
    # The AUR rejects the requests of the link checker.
    r"https://aur\.archlinux\.org/.*",
]

# GitHub serves file pages as JavaScript-rendered fragments, so the checker
# cannot see the anchors of a heading in a Markdown file.
linkcheck_anchors_ignore_for_url = [r"https://github\.com/.*"]
linkcheck_timeout = 30
linkcheck_retries = 2
