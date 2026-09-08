# Nautilus extension: "Open in yate" for folders (right-click on a folder or the background).
# Install: make install-nautilus  (copies to ~/.local/share/nautilus-python/extensions and restarts Nautilus)
# Requires the nautilus-python package.
import os
import shutil
import subprocess
from urllib.parse import unquote, urlparse

from gi.repository import GObject, Nautilus

YATE = shutil.which("yate") or os.path.expanduser("~/.local/bin/yate")


def _path(file_info):
    if file_info.get_uri_scheme() != "file":
        return None
    return unquote(urlparse(file_info.get_uri()).path)


class YateMenuProvider(GObject.GObject, Nautilus.MenuProvider):
    def _item(self, path, name="yate-open"):
        item = Nautilus.MenuItem(name=name, label="Open in yate", tip=f"Open a terminal in {path}")
        item.connect("activate", lambda _i: subprocess.Popen([YATE, path], start_new_session=True))
        return item

    def get_file_items(self, *args):
        files = args[-1]  # nautilus-python 4: (files,); 3: (window, files)
        dirs = [p for f in files if f.is_directory() and (p := _path(f))]
        if not dirs:
            return []
        return [self._item(dirs[0])]

    def get_background_items(self, *args):
        folder = args[-1]
        path = _path(folder)
        return [self._item(path, "yate-open-here")] if path else []
