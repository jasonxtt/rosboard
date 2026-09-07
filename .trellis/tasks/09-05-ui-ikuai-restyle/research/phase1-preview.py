#!/usr/bin/env python3
"""Isolated read-only fixture API for reviewing the actual compact React build.

Run with Python, then point ROSBOARD_DEV_PROXY at http://127.0.0.1:18792.
No RouterOS connection, credentials, persistent writes, or production API proxy.
"""
import json
import math
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse


def fixture(device_id, window):
    now = datetime.now(timezone.utc)
    stamp = now.isoformat()
    duration = {'5m': 300, '30m': 1800, '1h': 3600, '1d': 86400}.get(window, 300)
    secondary = device_id == 'preview-2'
    count = 18 if secondary else 42
    samples = [dict(timestamp=(now - timedelta(seconds=(59-i)*duration/59)).isoformat(),
                    uploadBps=round(1_000_000 + 650_000 * (1 + math.sin(i*.6))),
                    downloadBps=round(9_000_000 + 7_000_000 * (1 + math.sin(i*.35))),
                    cpuLoadPercent=round(12 + 9 * (1 + math.sin(i*.5))),
                    memoryUsedPercent=round(34 + 2*math.sin(i*.2), 1),
                    storageUsedPercent=21.5, onlineTerminalCount=count + i % 4,
                    connectionCount=610 + (i*31) % 430) for i in range(60)]
    latest = samples[-1]
    overview = dict(routerName='Rosboard-Lab-2' if secondary else 'Rosboard-Lab',
                    platform='MikroTik', version='7.20.1', boardName='RB5009UG+S+',
                    uptime='12d 06:28:15', memoryUsedBytes=365072220,
                    memoryTotalBytes=1073741824, storageUsedBytes=27500000,
                    storageTotalBytes=128000000, connectedDeviceCount=latest['onlineTerminalCount'],
                    terminalStateCounts=dict(online=latest['onlineTerminalCount'], inactive=6, offline=9),
                    connectionProtocolCounts=dict(tcp=630, udp=80, other=9),
                    trafficInterfaces=['ether1'], healthEnabled=True, updatedAt=stamp,
                    chartSamples=samples, **{k: v for k, v in latest.items() if k not in ('timestamp', 'onlineTerminalCount')})
    overview['connectionCount'] = 719
    interfaces = [dict(name=f'ether{i}', type='ether', running=i != 4, disabled=i == 5,
                       macAddress=f'02:00:00:00:00:{i:02x}', status='', lastLinkUpTime=stamp,
                       linkDowns=0, actualMtu=1500, rxBytes=9200000000*i, txBytes=420000000*i,
                       currentRxBps=latest['downloadBps'] if i == 1 else 120000*i,
                       currentTxBps=latest['uploadBps'] if i == 1 else 24000*i,
                       addresses=['192.0.2.2/24'] if i == 1 else [],
                       rxPackets=400000, txPackets=100000, rxDrops=0, txDrops=0,
                       rxErrors=0, txErrors=0, linkRate='1 Gbps', fullDuplex=True,
                       category='physical', relations=[]) for i in range(1, 6)]
    devices = [dict(id=f'preview-{i}', name='紧凑界面预览 · 模拟设备' if i == 1 else '分支网络 · 长名称测试设备',
                    enabled=True, archived=False, healthy=True, routerName=f'Rosboard-Lab-{i}',
                    version='7.20.1', updatedAt=stamp) for i in (1, 2)]
    settings = dict(connection=dict(apiBasePath='/rest', configured=True, listenAddress='127.0.0.1:18792',
                    allowedCidrs=[], routerosBaseUrl='http://192.0.2.2', routerosScheme='http',
                    routerosHost='192.0.2.2', routerosPort=80, routerosUsername='', routerosPasswordSet=False),
                    collection=dict(pollIntervalSeconds=10, realtimePollIntervalSeconds=1,
                    terminalPollIntervalSeconds=10, sampleRetentionHours=24),
                    diagnostics=dict(routerName=overview['routerName'], version=overview['version'], updatedAt=stamp),
                    devices=[dict(**d, scheme='http', host=f'192.0.2.{i+1}', port=80, username='',
                    passwordSet=False, cleanupAvailable=False, trafficInterfaces=['ether1'],
                    terminalCidrs=['198.51.100.0/24'], protocolAnalysis=False) for i, d in enumerate(devices)])
    fleet = [dict(**d, state='online', alerting=False, address=f'192.0.2.{i+1}',
                  platform=overview['platform'], boardName=overview['boardName'],
                  cpuLoadPercent=latest['cpuLoadPercent'], memoryUsedPercent=latest['memoryUsedPercent'],
                  uploadBps=latest['uploadBps'], downloadBps=latest['downloadBps'], terminalCount=count,
                  terminalOnline=count, terminalInactive=6, terminalOffline=9, connectionCount=719,
                  connectionTCP=630, connectionUDP=80, connectionOther=9, uptime=overview['uptime'])
             for i, d in enumerate(devices)]
    terminals = [dict(id=f'lab-{i}', displayName=name, autoName=name, customName='', remark='模拟终端',
                      macAddress=f'02:00:00:00:01:{i:02x}', primaryInterface='ether2',
                      ipv4=[f'198.51.100.{i+8}'], ipv6=[f'2001:db8::{i}'],
                      routingIpv4=[f'198.51.100.{i+8}'], routingIpv6=[f'2001:db8::{i}'], autoEligible=True,
                      connectionCount=12*i, currentUploadBps=12000*i, currentDownloadBps=400000*i,
                      totalUploadBytes=1000000*i, totalDownloadBytes=6000000*i,
                      trackingSince=stamp, lastSeen=stamp, primaryIpv4=f'198.51.100.{i+8}',
                      primaryIpv6=f'2001:db8::{i}', state='online', onlineSince=stamp, familyStats={})
                 for i, name in enumerate(['办公电脑', '客厅平板', '测试手机'], 1)]
    overview['systemResource'] = dict(architectureName='arm64', cpuCount='4', cpuFrequency='1400',
                                     cpu='ARM', boardName=overview['boardName'], cpuCores=[], irqs=[], hardware=[])
    dashboard = dict(overview=overview, interfaces=interfaces, terminals=terminals, protocols=[], policies=[],
                     routes=[], capabilities=[], warnings=[], alerts=[], dhcp=dict(servers=[], pools=[], leases=[]))
    data = {'/api/bootstrap': dict(phase='ready', username='preview'), '/api/devices': dict(devices=devices),
            '/api/settings': settings, '/api/dashboard': dashboard, '/api/realtime': overview,
            '/api/load': dict(samples=samples), '/api/traffic-history': dict(samples=samples),
            '/api/fleet-overview': dict(totalDevices=2, onlineDevices=2, offlineDevices=0, alertDevices=0, devices=fleet)}
    targets = [dict(id=f'target-{kind}', name=name, kind=kind, sourceType='manual', schedule='24h',
                    enabled=True, activeVersionId='v1', revision=1, pendingDeletion=False,
                    counts=dict(validRules=3), usage=dict(routingRuleCount=1, accessRuleCount=0),
                    versions=[], editableContent='example.com' if kind == 'domain' else '203.0.113.0/24')
               for kind, name in [('domain', '办公域名'), ('ip', '测试网段')]]
    ingress = dict(interfaceLists=[], interfaces=['ether2'])
    subject = dict(mode='selected', members=[dict(terminalId='lab-1', binding='auto', pinnedIpv4=[], pinnedIpv6=[])], prefixes=[])
    egress = dict(id='egress-1', name='主线路', priority=1, enabled=True, pendingDeletion=False, revision=1,
                  applied=True, failureMode='strict', routerOutput=False, families=[dict(family='ipv4', enabled=True,
                  wanInterface='ether1', gateway='192.0.2.1', routeTable='lab', routeMode='strict', natMode='', wanSource='')])
    data.update({
        '/api/target-lists': dict(targetLists=targets),
        '/api/application-presets': dict(presets=[]),
        '/api/policy-routing/overview': dict(egresses=[egress], trafficIngress=ingress),
        '/api/policy-routing/rules': dict(rules=[dict(id='rule-1', name='办公设备走主线路', subject=subject,
            ingress=ingress, targetListIds=['target-domain'], egressId='egress-1', priority=10, enabled=True, revision=1)]),
        '/api/policy-routing/discovery': dict(available=True, warnings=[], wans=[dict(interface='ether1',
            type='ether', running=True, pointToPoint=False, proven=True, routes=[dict(family='ipv4',
            destination='0.0.0.0/0', gateway='192.0.2.1', immediateGateway='192.0.2.1%ether1',
            table='main', active=True, proven=True)])], trafficIngress=[dict(name='ether2', kind='interface',
            include=[], exclude=[], staticMembers=[], dynamicMembers=False, frozen=False,
            addresses=['198.51.100.1/24'], reason='模拟 LAN 入口', coveredBy=[], default=True, dynamic=False, running=True)]),
        '/api/protocols': dict(protocols=[], history=[]),
    })
    for device in devices:
        data[f"/api/access-control/devices/{device['id']}"] = dict(device=device, terminals=terminals,
            targetLists=targets, rules=[dict(id='access-1', name='测试设备互联网限制', subject=subject,
            targetScope='internet', targetListIds=[], enabled=False, revision=1, members=[], status='disabled', issues=[])],
            state=dict(desiredRevision=1, appliedRevision=1), boundary='仅作用于受控设备的转发流量，局域网访问不受影响。')
    for item in targets:
        data[f"/api/target-lists/{item['id']}"] = item
    for item in interfaces:
        data[f"/api/interfaces/{item['name']}"] = dict(interface=item, samples=samples)
    for item in terminals:
        data[f"/api/terminals/{item['id']}"] = dict(terminal=item, connections=[], history=samples,
            capabilities=[], flowCategories=[], familySummaries={}, routeInterfaces=[], egressInterfaces=[])
    return data



class Handler(BaseHTTPRequestHandler):
    def reply(self, code, data):
        body = json.dumps(data).encode()
        self.send_response(code)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        url = urlparse(self.path)
        query = parse_qs(url.query)
        data = fixture(query.get('device', ['preview-1'])[0], query.get('window', ['5m'])[0])
        if url.path in data:
            self.reply(200, data[url.path])
        else:
            self.reply(501, dict(error='此隔离预览未模拟该接口；没有连接真实设备。'))

    def do_POST(self):
        if self.command == 'POST' and urlparse(self.path).path == '/api/policy-routing/plans':
            # A display fixture only; applying it still returns the write-rejection below.
            self.reply(200, dict(planId='preview-plan', planHash='synthetic-preview', readOnly=True,
                plan=dict(planID='preview-plan', planHash='synthetic-preview', kind='routing', state='preview',
                desiredRevision=1, actualFingerprint='synthetic', lifecycle='prepared',
                createdAt=datetime.now(timezone.utc).isoformat(), blockers=[], familyBlockers=[],
                warnings=[dict(code='preview_only', reason='这是模拟变更计划，仅用于检查界面，不会写入设备。')],
                pendingReview=False, acknowledgements=[dict(code='确认模拟预览', required=True, accepted=False)],
                operations=[dict(seq=1, phase='routing', action='create', menu='routing/table', family='ipv4',
                    after=dict(comment='preview | 模拟策略路由表'))], executionGroups=[], summary={})))
        elif urlparse(self.path).path in ('/api/viewer-heartbeat', '/api/terminal-viewer-heartbeat'):
            self.reply(200, {})
        else:
            self.reply(405, dict(error='模拟数据预览不执行业务写入。'))

    do_PUT = do_POST
    do_PATCH = do_POST
    do_DELETE = do_POST

    def log_message(self, *_args):
        pass


if __name__ == '__main__':
    ThreadingHTTPServer(('127.0.0.1', 18792), Handler).serve_forever()
