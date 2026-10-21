"""ichava-maintainer-toolkit: maintainer toolkit for the Ichava icon-pack ecosystem.

.. deprecated:: 0.2.0
    The Python implementation is deprecated. The Go binary ``icon-sets-sync``
    (module ``src/``, entrypoint ``src/cmd/icon-sets-sync``) replaces it and is
    what the Docker image runs from 0.2.0. Every public name below still
    imports and works; the package keeps its tests and its CI job until the
    owner approves removing it. Earliest release that could remove it: 0.3.0.
    The per-symbol record is in the design folder's
    ``audits/deprecations.md``.

Public API surface -- import these directly when wiring a custom recipe:

    from ichava_maintainer_toolkit import Pipeline, sources, transforms, sinks

    Pipeline.named("my-pack")
        .source(sources.NpmTarball("@x/y", version="1.0.0"))
        .transform(transforms.Sanitise())
        .sink(sinks.Filesystem(root="/work/x/resources/assets/svg/files"))
        .run()

Everything else lives under `ichava_maintainer_toolkit.core.*` and `ichava_maintainer_toolkit.recipes.*`.
"""

from ichava_maintainer_toolkit.core import sinks, sources, transforms
from ichava_maintainer_toolkit.core.pipeline import Pipeline, Stage, StageContext
from ichava_maintainer_toolkit.version import __version__

__all__ = [
    "Pipeline",
    "Stage",
    "StageContext",
    "__version__",
    "sinks",
    "sources",
    "transforms",
]
