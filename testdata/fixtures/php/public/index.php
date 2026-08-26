<?php
$path = parse_url($_SERVER['REQUEST_URI'] ?? '/', PHP_URL_PATH);
if ($path === '/redirect') { header('Location: /target', true, 302); exit; }
if ($path === '/cookie') { setcookie('devlan_fixture', 'ok', ['path' => '/']); }
if ($path === '/asset.css') { header('Content-Type: text/css'); echo 'body{color:#123456}'; exit; }
if ($path === '/origin') { header('Content-Type: application/json'); echo json_encode(['host'=>$_SERVER['HTTP_HOST'] ?? '', 'origin'=>$_SERVER['HTTP_ORIGIN'] ?? '', 'upgrade'=>$_SERVER['HTTP_UPGRADE'] ?? '']); exit; }
header('Content-Type: text/html; charset=utf-8');
echo '<!doctype html><title>DevLAN PHP fixture</title><link rel="stylesheet" href="/asset.css"><p>php</p>';
