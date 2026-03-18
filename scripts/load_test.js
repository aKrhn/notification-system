import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

// Custom metrics
const notificationsCreated = new Counter('notifications_created');
const notificationsFailed = new Counter('notifications_failed');
const createDuration = new Trend('create_duration', true);
const listDuration = new Trend('list_duration', true);

export const options = {
    scenarios: {
        // Scenario 1: Sustained load — normal traffic
        sustained_load: {
            executor: 'ramping-vus',
            startVUs: 0,
            stages: [
                { duration: '30s', target: 50 },
                { duration: '1m',  target: 50 },
                { duration: '10s', target: 0 },
            ],
            gracefulRampDown: '10s',
        },
        // Scenario 2: Burst — simulate flash sale / breaking news
        burst_traffic: {
            executor: 'ramping-vus',
            startVUs: 0,
            startTime: '1m40s', // Start after sustained load
            stages: [
                { duration: '10s', target: 200 },
                { duration: '30s', target: 200 },
                { duration: '10s', target: 0 },
            ],
            gracefulRampDown: '10s',
        },
        // Scenario 3: Batch creation stress
        batch_stress: {
            executor: 'per-vu-iterations',
            vus: 10,
            iterations: 5,
            startTime: '2m30s',
            maxDuration: '1m',
        },
    },
    thresholds: {
        http_req_duration: ['p(95)<500', 'p(99)<1000'],
        http_req_failed: ['rate<0.05'],
        create_duration: ['p(95)<300'],
        list_duration: ['p(95)<200'],
    },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const channels = ['sms', 'email', 'push'];
const priorities = ['high', 'normal', 'low'];

function randomChannel() {
    return channels[Math.floor(Math.random() * channels.length)];
}

function randomPriority() {
    return priorities[Math.floor(Math.random() * priorities.length)];
}

function randomRecipient(channel) {
    switch (channel) {
        case 'sms':
            return `+9055512${Math.floor(Math.random() * 100000).toString().padStart(5, '0')}`;
        case 'email':
            return `user${Math.floor(Math.random() * 100000)}@loadtest.com`;
        case 'push':
            return `device-token-${Math.floor(Math.random() * 100000)}`;
    }
}

export default function () {
    const scenario = __ENV.SCENARIO || exec.scenario.name;

    if (scenario === 'batch_stress') {
        batchCreateTest();
    } else {
        // Mix of operations: 60% create, 20% list, 10% get, 10% health
        const roll = Math.random();
        if (roll < 0.6) {
            createNotification();
        } else if (roll < 0.8) {
            listNotifications();
        } else if (roll < 0.9) {
            getNotification();
        } else {
            healthCheck();
        }
    }

    sleep(0.05 + Math.random() * 0.1); // 50-150ms between requests
}

import exec from 'k6/execution';

function createNotification() {
    const channel = randomChannel();
    const payload = JSON.stringify({
        recipient: randomRecipient(channel),
        channel: channel,
        content: `Load test ${Date.now()} - VU ${exec.vu.idInTest}`,
        priority: randomPriority(),
        subject: channel === 'email' ? 'Load Test Email' : undefined,
        metadata: { test_run: exec.scenario.name, vu: exec.vu.idInTest },
    });

    const res = http.post(`${BASE_URL}/api/v1/notifications`, payload, {
        headers: { 'Content-Type': 'application/json' },
        tags: { name: 'create_notification' },
    });

    createDuration.add(res.timings.duration);

    const ok = check(res, {
        'create: status 201': (r) => r.status === 201,
        'create: has id': (r) => {
            try { return JSON.parse(r.body).id !== undefined; }
            catch { return false; }
        },
        'create: status is queued': (r) => {
            try { return JSON.parse(r.body).status === 'queued'; }
            catch { return false; }
        },
    });

    if (ok) {
        notificationsCreated.add(1);
    } else {
        notificationsFailed.add(1);
    }
}

function listNotifications() {
    const filters = [
        '',
        '?status=sent&limit=10',
        '?channel=sms&limit=20',
        '?priority=high&limit=5',
        '?status=pending&channel=email&limit=10',
    ];
    const filter = filters[Math.floor(Math.random() * filters.length)];

    const res = http.get(`${BASE_URL}/api/v1/notifications${filter}`, {
        tags: { name: 'list_notifications' },
    });

    listDuration.add(res.timings.duration);

    check(res, {
        'list: status 200': (r) => r.status === 200,
        'list: has data array': (r) => {
            try { return Array.isArray(JSON.parse(r.body).data); }
            catch { return false; }
        },
        'list: has pagination': (r) => {
            try { return JSON.parse(r.body).pagination !== undefined; }
            catch { return false; }
        },
    });
}

function getNotification() {
    // First create one, then get it
    const channel = randomChannel();
    const createRes = http.post(`${BASE_URL}/api/v1/notifications`, JSON.stringify({
        recipient: randomRecipient(channel),
        channel: channel,
        content: 'Get test',
        subject: channel === 'email' ? 'Test' : undefined,
    }), {
        headers: { 'Content-Type': 'application/json' },
        tags: { name: 'create_for_get' },
    });

    if (createRes.status !== 201) return;

    const id = JSON.parse(createRes.body).id;
    const res = http.get(`${BASE_URL}/api/v1/notifications/${id}`, {
        tags: { name: 'get_notification' },
    });

    check(res, {
        'get: status 200': (r) => r.status === 200,
        'get: correct id': (r) => {
            try { return JSON.parse(r.body).id === id; }
            catch { return false; }
        },
    });
}

function healthCheck() {
    const res = http.get(`${BASE_URL}/health`, {
        tags: { name: 'health_check' },
    });

    check(res, {
        'health: status 200': (r) => r.status === 200,
        'health: all ok': (r) => {
            try { return JSON.parse(r.body).status === 'healthy'; }
            catch { return false; }
        },
    });
}

function batchCreateTest() {
    const batchSizes = [10, 50, 100, 500];
    const size = batchSizes[Math.floor(Math.random() * batchSizes.length)];

    const notifications = [];
    for (let i = 0; i < size; i++) {
        const channel = randomChannel();
        notifications.push({
            recipient: randomRecipient(channel),
            channel: channel,
            content: `Batch stress test item ${i}`,
            priority: randomPriority(),
            subject: channel === 'email' ? 'Batch Test' : undefined,
        });
    }

    const res = http.post(`${BASE_URL}/api/v1/notifications/batch`, JSON.stringify({
        notifications: notifications,
    }), {
        headers: { 'Content-Type': 'application/json' },
        tags: { name: 'batch_create' },
        timeout: '30s',
    });

    check(res, {
        'batch: status 201': (r) => r.status === 201,
        'batch: correct count': (r) => {
            try { return JSON.parse(r.body).count === size; }
            catch { return false; }
        },
        'batch: has batch_id': (r) => {
            try { return JSON.parse(r.body).batch_id !== undefined; }
            catch { return false; }
        },
    });
}
