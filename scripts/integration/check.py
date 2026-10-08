"""Real HTTP -> SQLite -> SSH -> node tool -> Caddy -> usage round-trip.
Only targets the isolated compose project on loopback ports 18088/18089.
"""
import base64
import http.cookiejar
import json
import os
import socket
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
TEST = ROOT / '.test' / 'integration'
compose = ['sudo', '-n', 'docker', 'compose', '-p', 'naive-mvp-test', '-f', str(TEST / 'compose.yaml')]
jar = http.cookiejar.CookieJar()
http = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
csrf = ''
def api(path, payload=None, method=None, expect=200):
    req = urllib.request.Request('http://127.0.0.1:18088' + path,
        data=None if payload is None else json.dumps(payload).encode(),
        headers={'Content-Type': 'application/json', 'X-CSRF-Token': csrf}, method=method)
    try:
        response = http.open(req, timeout=90)
    except urllib.error.HTTPError as e:
        response = e
    data = response.read()
    assert response.status == expect, (path, response.status, data)
    return json.loads(data) if data and response.headers.get_content_type() == 'application/json' else data.decode()

def state(): return api('/api/state')
def connect(uri, expect=True):
    u = urllib.parse.urlsplit(uri)
    auth = base64.b64encode((urllib.parse.unquote(u.username) + ':' + urllib.parse.unquote(u.password)).encode()).decode()
    conn = socket.create_connection(('127.0.0.1', 18089), timeout=10)
    conn.sendall(('CONNECT 127.0.0.1:9090 HTTP/1.1\r\nHost: 127.0.0.1:9090\r\nProxy-Authorization: Basic ' + auth + '\r\n\r\n').encode())
    response = b''
    while b'\r\n\r\n' not in response:
        chunk = conn.recv(4096)
        assert chunk
        response += chunk
    assert (b' 200 ' in response.split(b'\r\n')[0]) == expect, response
    return conn

def echo(conn, payload):
    conn.sendall(payload)
    result = b''
    while len(result) < len(payload): result += conn.recv(len(payload) - len(result))
    assert result == payload

def login():
    global csrf
    csrf = api('/api/login', {'password': (TEST / 'admin_password').read_text().strip()})['csrf']

for _ in range(40):
    try:
        api('/healthz')
        break
    except Exception:
        time.sleep(0.5)
else: raise RuntimeError('dashboard did not start')
login()
assert not state()['nodes'], 'use a fresh test volume'
fp = subprocess.check_output(compose + ['exec', '-T', 'test-node', 'ssh-keygen', '-lf', '/keys/host.pub', '-E', 'sha256'], text=True).split()[1]
node = {'name':'Local test','host':'test-node','ssh_port':22,'ssh_user':'root','key_ref':'control','host_fingerprint':fp,'proxy_host':'127.0.0.1','proxy_port':18089,'protocol':'https'}
api('/api/nodes', node)
n = state()['nodes'][0]
api('/api/nodes/' + n['id'] + '/sync', {})
assert state()['nodes'][0]['health'] == 'healthy'
api('/api/users', {'name':'Integration','enabled':True,'expires_at':'','quota_bytes':0,'grants':[n['id']]})
u = state()['users'][0]
api('/api/nodes/' + n['id'] + '/sync', {})
uri = api('/sub/' + u['subscription_token']).strip()
short = connect(uri); echo(short,b'short-payload'); short.close()
long = connect(uri); echo(long,b'active-payload-' * 100)
print('Short tunnel complete; waiting for a real 60-second active snapshot.', flush=True)
time.sleep(63)
api('/api/nodes/' + n['id'] + '/collect', {})
u = state()['users'][0]
expected = len(b'short-payload') + len(b'active-payload-' * 100)
assert (u['total_up'],u['total_down']) == (expected,expected), u
# Reload while the long tunnel remains open; old credentials are revoked for new tunnels.
api('/api/users/' + u['id'] + '/rotate', {'kind':'proxy'})
api('/api/nodes/' + n['id'] + '/sync', {})
blocked = connect(uri, False); blocked.close()
echo(long,b'after-reload'); long.close()
time.sleep(1)
api('/api/nodes/' + n['id'] + '/collect', {})
expected += len(b'after-reload')
u = state()['users'][0]
assert (u['total_up'],u['total_down']) == (expected,expected), u
api('/api/nodes/' + n['id'] + '/collect', {})
assert state()['users'][0]['total_up'] == expected
# Token rotation leaves the current proxy URI unchanged.
new_uri = api('/sub/' + u['subscription_token']).strip()
api('/api/users/' + u['id'] + '/rotate', {'kind':'subscription'})
api('/sub/' + u['subscription_token'], expect=404)
u = state()['users'][0]
assert api('/sub/' + u['subscription_token']).strip() == new_uri
# Wrong host fingerprint fails closed.
wrong = dict(node,host_fingerprint='SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA')
api('/api/nodes/' + n['id'],wrong,'PUT')
result=api('/api/nodes/' + n['id'] + '/sync',{},expect=502)
assert result['error']=='host_key_mismatch',result
api('/api/nodes/' + n['id'],node,'PUT')
# Soft quota revokes credentials and suppresses subscription.
edit = {k:u[k] for k in ['name','enabled','expires_at','grants']}
edit['quota_bytes'] = expected * 2
api('/api/users/' + u['id'],edit,'PUT')
assert state()['users'][0]['quota_state']=='exceeded'
api('/sub/' + u['subscription_token'],expect=404)
api('/api/nodes/' + n['id'] + '/sync',{})
blocked=connect(new_uri,False);blocked.close()
# Recreate only the central service. Its DB, usage and cursor must survive.
subprocess.run(compose + ['up','-d','--force-recreate','dashboard'],check=True,stdout=subprocess.DEVNULL)
for _ in range(40):
    try: api('/healthz');break
    except Exception: time.sleep(0.5)
login()
u=state()['users'][0]
assert (u['total_up'],u['total_down'])==(expected,expected)
api('/api/nodes/' + n['id'] + '/collect',{})
assert state()['users'][0]['total_up']==expected
print(json.dumps({'result':'PASS','up':expected,'down':expected,'checks':['forced SSH RPC','host fingerprint rejection','empty-user deny','short final','active 60s snapshot','reload old tunnel','new-request revocation','rotation independence','dedup retry','soft quota','container persistence']},ensure_ascii=False))
