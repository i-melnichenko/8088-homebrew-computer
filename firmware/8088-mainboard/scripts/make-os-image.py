#!/usr/bin/env python3
"""Build a deterministic 1.44-MiB FAT12 floppy with a reserved-area kernel."""
import struct
import sys
from pathlib import Path


def build(boot, kernel, files):
    if len(boot) != 512 or boot[510:] != b'\x55\xaa':
        raise ValueError('boot sector must be 512 bytes with signature 55 AA')
    if not 0 < len(kernel) <= 8192:
        raise ValueError('kernel must fit in 16 reserved sectors')
    if len(files) > 224:
        raise ValueError('root directory is full')
    image = bytearray(2880 * 512)
    image[:512] = boot
    image[512:512+len(kernel)] = kernel
    fat = bytearray(9 * 512)
    fat[:3] = b'\xf0\xff\xff'
    cluster = 2
    names = set()
    for index, (name, data) in enumerate(files):
        parts = name.upper().split('.')
        if (len(parts) != 2 or not 1 <= len(parts[0]) <= 8
                or not 1 <= len(parts[1]) <= 3
                or any(c not in 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_' for p in parts for c in p)):
            raise ValueError(f'invalid 8.3 filename: {name}')
        packed = (parts[0].ljust(8) + parts[1].ljust(3)).encode('ascii')
        if packed in names:
            raise ValueError(f'duplicate filename: {name}')
        names.add(packed)
        if not 0 < len(data) <= 0x7b00:
            raise ValueError(f'{name}: COM size must be 1..31488 bytes')
        count = (len(data)+511)//512
        if cluster+count > 2833:
            raise ValueError('disk is full')
        entry = 35*512 + index*32
        image[entry:entry+11] = packed
        image[entry+11] = 0x20
        struct.pack_into('<H', image, entry+26, cluster)
        struct.pack_into('<I', image, entry+28, len(data))
        offset = (49+cluster-2)*512
        image[offset:offset+len(data)] = data
        for c in range(cluster, cluster+count):
            value = c+1 if c+1 < cluster+count else 0xfff
            pos = c+c//2
            word = struct.unpack_from('<H', fat, pos)[0]
            word = (word & 0x000f) | (value << 4) if c & 1 else (word & 0xf000) | value
            struct.pack_into('<H', fat, pos, word)
        cluster += count
    image[17*512:26*512] = fat
    image[26*512:35*512] = fat
    return bytes(image)


if __name__ == '__main__':
    out, boot, kernel, *programs = map(Path, sys.argv[1:])
    out.write_bytes(build(boot.read_bytes(), kernel.read_bytes(), [(p.name, p.read_bytes()) for p in programs]))
    print(f'{out}: FAT12, 1440 KiB, {len(programs)} programs')
