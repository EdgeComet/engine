import {defineConfig} from 'vitepress'
import {withMermaid} from 'vitepress-plugin-mermaid'
import vclGrammar from './vcl.tmLanguage.json'

// https://vitepress.dev/reference/site-config
// @ts-ignore
export default withMermaid(defineConfig({
    base: '/docs/',
    title: "EdgeComet",
    description: "A powerful JS rendering & cache system",
    markdown: {
        languages: [vclGrammar as any]
    },
    head: [
        ['link', {rel: 'icon', type: 'image/png', href: '/docs/images/favicon.png'}]
    ],
    themeConfig: {
        logo: {
            light: '/images/logo_black_150x30.png',
            dark: '/images/logo_white_150x30.png',
        },
        siteTitle: false,
        // https://vitepress.dev/reference/default-theme-config
        nav: [
            {text: 'Home', link: '/'},
            {text: 'Quick Start', link: '/quick-start'},
            {text: 'API Reference', link: '/cache-daemon/api-reference'}
        ],

        sidebar: [
            {
                text: 'Overview',
                items: [
                    {text: 'Introduction', link: '/'},
                    {text: 'Use Cases', link: '/use-cases'},
                ]
            },
            {
                text: 'Installation',
                items: [
                    {text: 'Topology', link: '/getting-started/topology'},
                    {text: 'Installation', link: '/getting-started/installation'},
                    {text: 'Quick Config', link: '/getting-started/configuration'},
                    {text: 'Systemd Setup', link: '/getting-started/systemd-setup'},
                ]
            },
            {
                text: 'Integrations',
                collapsed: false,
                items: [
                    {text: 'Nginx', link: '/integrations/nginx'},
                    {text: 'Cloudflare Worker', link: '/integrations/cloudflare-worker'},
                    {text: 'Fastly CDN', link: '/integrations/fastly'}
                ]
            },
            {
                text: 'Edge Gateway',
                collapsed: false,
                items: [
                    {text: 'Overview', link: '/edge-gateway/overview'},
                    {text: 'Request Flow', link: '/edge-gateway/request-flow'},
                    {text: 'URL Normalization', link: '/edge-gateway/url-normalization'},
                    {text: 'Render Mode', link: '/edge-gateway/render-mode'},
                    {text: 'Dimensions', link: '/edge-gateway/dimensions'},
                    {text: 'Bypass Mode', link: '/edge-gateway/bypass-mode'},
                    {text: 'Caching', link: '/edge-gateway/caching'},
                    {text: 'Sharding', link: '/edge-gateway/sharding'},
                    {text: 'URL Rules', link: '/edge-gateway/url-rules'},
                    {text: 'Configuration', link: '/edge-gateway/configuration'},
                    {text: 'X-Headers', link: '/edge-gateway/x-headers'},
                    {text: 'Monitoring', link: '/edge-gateway/monitoring'}
                ]
            },
            {
                text: 'Render Service',
                collapsed: false,
                items: [
                    {text: 'Overview', link: '/render-service/overview'},
                    {text: 'Chrome Pool', link: '/render-service/chrome-pool'},
                    {text: 'Configuration', link: '/render-service/configuration'}
                ]
            },
            {
                text: 'Cache Daemon',
                collapsed: false,
                items: [
                    {text: 'Overview', link: '/cache-daemon/overview'},
                    {text: 'Config Reference', link: '/cache-daemon/config-reference'},
                    {text: 'API Reference', link: '/cache-daemon/api-reference'}
                ]
            },
            {
                text: 'Troubleshooting',
                collapsed: false,
                items: [
                    {text: 'Overview', link: '/troubleshooting/overview'},
                    {text: 'Rendering Failures', link: '/troubleshooting/rendering-failures'},
                    //{text: 'Cache Issues', link: '/troubleshooting/cache-issues'},
                    {text: 'Chrome Pool', link: '/troubleshooting/chrome-pool'},
                    {text: 'Log Reference', link: '/troubleshooting/log-reference'}
                ]
            },
            {
                text: 'Reference',
                collapsed: false,
                items: [
                    {text: 'X-Headers', link: '/edge-gateway/x-headers'},
                    {text: 'Ports', link: '/reference/ports'},
                    {text: 'Metrics', link: '/reference/metrics'},
                    {text: 'API', link: '/cache-daemon/api-reference'}
                ]
            }
        ],

        socialLinks: [
            {icon: 'github', link: 'https://github.com/EdgeComet/engine'}
        ]
    }
}))
