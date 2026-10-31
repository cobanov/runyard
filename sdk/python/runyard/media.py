"""Turns images and videos into files. numpy, Pillow and imageio are optional: without Pillow, PNGs are written by hand."""
import os
import shutil
import struct
import zlib
from pathlib import Path


def _copy(src, path):
    src = Path(src)
    dst = path.with_suffix(src.suffix.lower() or path.suffix)
    shutil.copyfile(src, dst)
    return dst


def to_array(obj):
    """HxW or HxWxC uint8 array from a numpy array or torch tensor; CHW, [0, 1], [-1, 1] and [0, 255] floats are handled."""
    import numpy as np

    if hasattr(obj, "detach"):  # torch
        obj = obj.detach().float().cpu().numpy()
    a = np.asarray(obj)
    if a.ndim == 3 and a.shape[0] in (1, 3, 4) and a.shape[-1] not in (1, 3, 4):
        a = a.transpose(1, 2, 0)
    if a.ndim == 3 and a.shape[-1] == 1:
        a = a[..., 0]
    if a.dtype != np.uint8:
        a = a.astype(np.float32)
        if a.size and a.max() > 1.0 + 1e-6:
            a = a / 255.0
        elif a.size and a.min() < 0:
            a = (a + 1) / 2
        a = (np.clip(a, 0, 1) * 255).round().astype(np.uint8)
    return a


def write_png(a, path):
    """Encodes an HxW, HxWx3 or HxWx4 uint8 array as PNG with zlib alone."""
    h, w = a.shape[:2]
    channels = 1 if a.ndim == 2 else a.shape[-1]
    ctype = {1: 0, 3: 2, 4: 6}.get(channels) if a.ndim in (2, 3) else None  # gray, RGB, RGBA
    if ctype is None:
        raise ValueError(f"cannot make an image of shape {a.shape}")
    raw = b"".join(b"\x00" + a[y].tobytes() for y in range(h))

    def chunk(t, d):
        return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d))

    Path(path).write_bytes(b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, ctype, 0, 0, 0))
                           + chunk(b"IDAT", zlib.compress(raw, 6)) + chunk(b"IEND", b""))


def save_image(obj, path):
    """Writes a PIL image, array, tensor or image file to path (a .png); returns the path actually written."""
    path.parent.mkdir(parents=True, exist_ok=True)
    if isinstance(obj, (str, os.PathLike)):
        return _copy(obj, path)
    if hasattr(obj, "save") and hasattr(obj, "mode"):  # PIL
        obj.save(path, format="PNG")
        return path
    a = to_array(obj)
    try:
        from PIL import Image
    except ImportError:
        write_png(a, path)
    else:
        Image.fromarray(a).save(path)
    return path


def save_video(obj, path, fps):
    """A video file is copied; frames (arrays, tensors or PIL images) become an H.264 mp4 with imageio and imageio-ffmpeg,
    else an animated GIF, which needs Pillow."""
    path.parent.mkdir(parents=True, exist_ok=True)
    if isinstance(obj, (str, os.PathLike)):
        return _copy(obj, path)
    try:
        import imageio.v2 as imageio
        import imageio_ffmpeg  # noqa: F401  the backend imageio writes mp4 with
    except ImportError:
        from PIL import Image

        frames = [f if hasattr(f, "mode") else Image.fromarray(to_array(f)) for f in obj]
        path = path.with_suffix(".gif")
        frames[0].save(path, save_all=True, append_images=frames[1:], duration=max(1, round(1000 / fps)), loop=0)
        return path
    import numpy as np

    path = path.with_suffix(".mp4")
    with imageio.get_writer(path, fps=fps, codec="libx264", pixelformat="yuv420p", macro_block_size=1) as w:
        for f in obj:
            a = np.asarray(f.convert("RGB")) if hasattr(f, "mode") else to_array(f)
            # yuv420p needs even sides (Safari and iOS play nothing else): repeat the last row or column
            w.append_data(np.pad(a, ((0, a.shape[0] % 2), (0, a.shape[1] % 2)) + ((0, 0),) * (a.ndim - 2), mode="edge"))
    return path
