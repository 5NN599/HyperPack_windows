import argparse, os, subprocess, zipfile, time

p = argparse.ArgumentParser()
p.add_argument('input')
p.add_argument('--hpk', default='bench.hpk')
p.add_argument('--zip', default='bench.zip')
args = p.parse_args()

exe = os.path.abspath(os.path.join(os.path.dirname(__file__), '..', 'HyperPack.exe'))
root = os.path.abspath(args.input)

if os.path.isdir(root):
    cmd = [exe, '--pack-folder', args.hpk, root]
else:
    cmd = [exe, '--compress', root, args.hpk]

t0 = time.perf_counter(); subprocess.run(cmd, check=True); t1 = time.perf_counter()

with zipfile.ZipFile(args.zip, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as z:
    if os.path.isdir(root):
        base = os.path.dirname(root)
        for dp, _, fs in os.walk(root):
            for f in fs:
                pth = os.path.join(dp, f)
                z.write(pth, os.path.relpath(pth, base))
    else:
        z.write(root, os.path.basename(root))

print('input :', root)
print('HPK   :', os.path.getsize(args.hpk), 'bytes')
print('ZIP   :', os.path.getsize(args.zip), 'bytes')
print('HPK t :', round(t1-t0, 3), 'sec')
