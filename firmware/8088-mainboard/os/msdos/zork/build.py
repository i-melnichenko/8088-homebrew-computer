"""Build Infocom's generic DOS 2.0 interpreter for the UART console."""
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import urllib.request
import zipfile


def fetch_assets(cache):
    manifest = json.loads(Path(__file__).with_name('upstream.json').read_text())
    cache.mkdir(parents=True, exist_ok=True)
    result = {}
    for key, item in manifest.items():
        path = cache/item['file']
        if path.exists():
            data = path.read_bytes()
        else:
            print(f'Downloading Zork {key}', flush=True)
            with urllib.request.urlopen(item['url'], timeout=60) as response:
                data = response.read()
        if hashlib.sha256(data).hexdigest() != item['sha256']:
            raise ValueError(f'{path.name}: upstream SHA-256 mismatch; refusing to build')
        if not path.exists():
            path.write_bytes(data)
        result[key] = path
    return result


def run_logged(command, cwd, log, env=None):
    with log.open('w') as output:
        completed = subprocess.run(command, cwd=cwd, env=env, stdout=output, stderr=subprocess.STDOUT)
    if completed.returncode:
        raise RuntimeError(f'{command[0]} failed; see {log}\n' +
                           '\n'.join(log.read_text().splitlines()[-25:]))


def adapt_interpreter(source):
    # Keep the interpreter's VM and DOS routines; change its fixed FCB names.
    for old, new in (('WITNESS DAT', 'ZORK1   DAT'), ('WITNESS SAV', 'ZORK1   SAV')):
        if source.count(old) != 1:
            raise ValueError('unexpected Infocom filename declaration')
        source = source.replace(old, new)
    # Put uninitialized game RAM last: DOS allocates it from the MZ header,
    # saving 56 KiB of zero bytes on the UART disk without changing addresses
    # within any segment. Segment relocations are resolved by DOS's EXE loader.
    game = 'GAME_SG\tSEGMENT PARA\n\tDB\t0E000H DUP(?)\nGAME_SG\tENDS\n'
    end = '\tEND\tSTART'
    if source.count(game) != 1 or source.count(end) != 1:
        raise ValueError('unexpected Infocom segment layout')
    return source.replace(game, '').replace(end, game+'\n'+end)


def build_files(build_dir):
    cache = (build_dir/'zork-upstream').resolve()
    assets = fetch_assets(cache)
    with zipfile.ZipFile(assets['assembler']) as archive:
        directory = cache/archive.namelist()[0].split('/')[0]
        if not directory.exists():
            archive.extractall(cache)
    compiler = os.environ.get('CC')
    if not compiler:
        compiler = (subprocess.check_output(['xcrun', '--find', 'clang'], text=True).strip()
                    if platform.system() == 'Darwin' else 'cc')
    environment = os.environ.copy()
    # This is an independent upstream build. Inherited -B would rerun its
    # non-idempotent directory rule; it also owns its own parallel job count.
    for name in ('MAKEFLAGS', 'MFLAGS', 'MAKEOVERRIDES'):
        environment.pop(name, None)
    if platform.system() == 'Darwin' and not environment.get('SDKROOT'):
        environment['SDKROOT'] = subprocess.check_output(
            ['xcrun', '--sdk', 'macosx', '--show-sdk-path'], text=True).strip()
    print('Building JWasm (host C compiler and make required)', flush=True)
    run_logged(['make', '-f', 'GccUnix.mak', f'CC={compiler}', '-j2'], directory,
               cache/'jwasm-build.log', env=environment)
    source = cache/'zork1.asm'
    source.write_text(adapt_interpreter(assets['interpreter'].read_text()))
    executable = cache/'ZORK1.EXE'
    run_logged([str(directory/'GccUnixR/jwasm'), '-0', '-Zm', '-mz',
                f'-Fo{executable}', str(source)], cache, cache/'assemble.log')
    # SETUP.INF: 25 rows, 80 columns, plain ASCII (no ANSI status-line redraw).
    # Encoding is defined by SSVER="A"-10 and SSHOR=-6 in Infocom's source.
    return [('ZORK1.EXE', executable.read_bytes()),
            ('ZORK1.DAT', assets['story'].read_bytes()),
            ('SETUP.INF', b'PJN'),
            ('ZORKLIC.TXT', assets['license'].read_bytes())]
