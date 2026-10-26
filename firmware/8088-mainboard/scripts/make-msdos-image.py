#!/usr/bin/env python3
"""Build an OEM MS-DOS 2.0 floppy from pinned, unmodified Microsoft binaries."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import struct
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('fat12_image', ROOT/'scripts/make-os-image.py')
fat12 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fat12)


def upstream_files(cache, source=None):
    manifest = json.loads((ROOT/'os/msdos/upstream.json').read_text())
    cache.mkdir(parents=True, exist_ok=True)
    result = {}
    for name, item in manifest['files'].items():
        cached = cache/name
        if source is not None:
            data = (source/item['path']).read_bytes()
        elif cached.exists():
            data = cached.read_bytes()
        else:
            url = f"https://raw.githubusercontent.com/microsoft/MS-DOS/{manifest['commit']}/{item['path']}"
            print(f'Downloading {name} from Microsoft commit {manifest["commit"]}', flush=True)
            with urllib.request.urlopen(url, timeout=60) as response:
                data = response.read()
        if hashlib.sha256(data).hexdigest() != item['sha256']:
            raise ValueError(f'{name}: upstream SHA-256 mismatch; refusing to build')
        if not cached.exists():
            cached.write_bytes(data)
        result[name] = data
    return result


def build(boot, io, hello, upstream, extra_files=()):
    # IO loader reads the unmodified kernel contiguously from data sector 49.
    if len(upstream['MSDOS.SYS']) != 16690:
        raise ValueError('DOS bootstrap requires the pinned 16690-byte kernel')
    files = [
        ('MSDOS.SYS', upstream['MSDOS.SYS']),
        ('COMMAND.COM', upstream['COMMAND.COM']),
        ('AUTOEXEC.BAT', b'ECHO OFF\r\nVER\r\n'),
        ('HELLO.COM', hello),
        ('DEBUG.COM', upstream['DEBUG.COM']),
        ('HELLO.TXT', b'Reading FAT12 through Microsoft MS-DOS.\r\n'),
        ('LICENSE.TXT', upstream['LICENSE.TXT']),
    ]
    files.extend(extra_files)
    # DOS supports larger data files and MZ executables than our native COM OS.
    image = bytearray(fat12.build(boot, io, files, max_file_size=1440*1024))
    # Mark DOS kernel as a hidden read-only system file; leave COMMAND visible.
    image[35*512+11] = 7
    for index in range(len(files)):
        struct.pack_into('<H', image, 35*512+index*32+24, 0x21)  # 1980-01-01
    return bytes(image)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, help='local checkout of the pinned Microsoft repository')
    parser.add_argument('--build-dir', type=Path, default=ROOT/'build')
    parser.add_argument('--zork', action='store_true', help='build the optional Zork I disk')
    args = parser.parse_args()
    build_dir = args.build_dir
    upstream = upstream_files(build_dir/'msdos-upstream', args.source)
    extra_files = ()
    if args.zork:
        zork_spec = importlib.util.spec_from_file_location('msdos_zork', ROOT/'os/msdos/zork/build.py')
        zork = importlib.util.module_from_spec(zork_spec)
        zork_spec.loader.exec_module(zork)
        extra_files = zork.build_files(build_dir)
    image = build((build_dir/'msdos-boot.bin').read_bytes(),
                  (build_dir/'msdos-io.bin').read_bytes(),
                  (build_dir/'doshello.com').read_bytes(), upstream, extra_files)
    target = build_dir/('msdos20-zork.img' if args.zork else 'msdos20.img')
    target.write_bytes(image)
    print(f'{target}: Microsoft MS-DOS 2.00 / COMMAND 2.02, FAT12, read-only')


if __name__ == '__main__':
    main()
