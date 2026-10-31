"""Minimal HTTP client for the runyard REST API (standard library only)."""
import json
import math
import urllib.error
import urllib.request
import uuid
from pathlib import Path


class HTTPError(Exception):
    def __init__(self, status, body):
        super().__init__(f"HTTP {status}: {body[:300]}")
        self.status, self.body = status, body


def clean(v):
    """JSON-safe copy: NaN/±Inf become strings (JSON has no such numbers), tuples lists, unknown objects str."""
    if isinstance(v, float) and not math.isfinite(v):
        return "NaN" if math.isnan(v) else ("Infinity" if v > 0 else "-Infinity")
    if isinstance(v, dict):
        return {str(k): clean(x) for k, x in v.items()}
    if isinstance(v, (list, tuple)):
        return [clean(x) for x in v]
    if v is None or isinstance(v, (str, int, float, bool)):
        return v
    if hasattr(v, "item"):  # numpy / torch scalar
        try:
            return clean(v.item())
        except Exception:
            pass
    return str(v)


def _stream(head, path, tail):
    yield head
    with open(path, "rb") as f:
        while True:
            chunk = f.read(1 << 20)
            if not chunk:
                break
            yield chunk
    yield tail


def request(method, url, key="", body=None, file=None, timeout=10):
    """Sends body as JSON, or as multipart fields when file=(filename, bytes or Path) is given; a Path is streamed from
    disk, so a large checkpoint never sits in memory. Returns the decoded reply."""
    headers = {"Authorization": "Bearer " + key} if key else {}
    data = None
    if file is not None:
        boundary = uuid.uuid4().hex
        parts = []
        for k, v in (body or {}).items():
            parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{k}"\r\n\r\n{v}\r\n'.encode())
        parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{file[0]}"\r\n'
                     f"Content-Type: application/octet-stream\r\n\r\n".encode())
        head, tail = b"".join(parts), f"\r\n--{boundary}--\r\n".encode()
        if isinstance(file[1], Path):
            data = _stream(head, file[1], tail)
            headers["Content-Length"] = str(len(head) + file[1].stat().st_size + len(tail))
        else:
            data = head + file[1] + tail
        headers["Content-Type"] = "multipart/form-data; boundary=" + boundary
    elif body is not None:
        data = json.dumps(clean(body), allow_nan=False).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            raw = r.read()
    except urllib.error.HTTPError as e:
        raise HTTPError(e.code, e.read().decode(errors="replace")) from None
    return json.loads(raw) if raw else None
