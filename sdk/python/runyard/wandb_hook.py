"""Makes `import wandb` load runyard.wandb_shim. The runyard_wandb.pth file installed with the package calls install()
at interpreter start when RUNYARD_WANDB=1, so tools that only support W&B write to runyard without code changes."""
import importlib.abc
import importlib.machinery
import importlib.metadata
import sys
import types

VERSION = "0.21.0"


class _Dist(importlib.metadata.Distribution):
    """Package metadata for `wandb`: accelerate and diffusers check it before they use W&B."""

    def read_text(self, filename):
        if filename == "METADATA":
            return f"Metadata-Version: 2.1\nName: wandb\nVersion: {VERSION}\n"
        return None

    def locate_file(self, path):
        return path


class _Finder(importlib.abc.MetaPathFinder, importlib.abc.Loader):
    def find_spec(self, name, path=None, target=None):
        if name == "wandb" or name.startswith("wandb."):
            return importlib.machinery.ModuleSpec(name, self, is_package=True)
        return None

    def create_module(self, spec):
        if spec.name == "wandb":
            from . import wandb_shim

            return wandb_shim
        from .wandb_shim import stub_attr

        m = types.ModuleType(spec.name)
        m.__path__ = []
        m.__getattr__ = lambda attr: stub_attr(spec.name, attr)
        return m

    def exec_module(self, module):
        pass

    def find_distributions(self, context=importlib.metadata.DistributionFinder.Context()):
        if context.name and context.name.lower().replace("_", "-") == "wandb":
            return [_Dist()]
        return []


def install():
    if not any(isinstance(f, _Finder) for f in sys.meta_path):
        sys.meta_path.insert(0, _Finder())
        for name in [m for m in sys.modules if m == "wandb" or m.startswith("wandb.")]:
            del sys.modules[name]  # a real wandb imported before us would win otherwise
