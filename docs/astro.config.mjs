// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
	site: 'https://infrashift.github.io',
	base: '/mrman',
	// The page was renamed when the docs moved to merge-request terminology.
	// The old URL was published, so it redirects rather than 404s.
	redirects: {
		'/docs/guides/pull-requests': '/mrman/docs/guides/merge-requests/',
	},
	integrations: [
		starlight({
			title: 'mrman',
			description:
				'Terminal code review with vim keybindings, persistent sessions, live agent collaboration, and submission to GitHub, GitLab, Azure DevOps and Forgejo.',
			favicon: '/favicon.svg',
			customCss: ['./src/styles/custom.css'],
			social: [
				{
					icon: 'github',
					label: 'GitHub',
					href: 'https://github.com/infrashift/mrman',
				},
			],
			editLink: {
				baseUrl: 'https://github.com/infrashift/mrman/edit/main/docs/',
			},
			sidebar: [
				{
					label: 'Start Here',
					items: [
						{ label: 'Overview', slug: 'docs' },
						{ label: 'Getting Started', slug: 'docs/getting-started' },
						{ label: 'Reviewing Locally', slug: 'docs/guides/local-review' },
					],
				},
				{
					label: 'Merge Requests',
					items: [
						{ label: 'How MR Review Works', slug: 'docs/guides/merge-requests' },
						{ label: 'GitHub', slug: 'docs/forges/github' },
						{ label: 'GitHub Enterprise Server', slug: 'docs/forges/github-enterprise' },
						{ label: 'GitLab', slug: 'docs/forges/gitlab' },
						{ label: 'Azure DevOps', slug: 'docs/forges/azure-devops' },
						{ label: 'Forgejo & Gitea', slug: 'docs/forges/forgejo', badge: { text: 'Experimental', variant: 'caution' } },
						{ label: 'Codeberg', slug: 'docs/forges/codeberg', badge: { text: 'Experimental', variant: 'caution' } },
					],
				},
				{
					label: 'Patches',
					items: [
						{ label: 'Reviewing Patches', slug: 'docs/guides/patches' },
						{ label: 'Patch Workflows', slug: 'docs/guides/patch-workflows' },
					],
				},
				{
					label: 'Guides',
					items: [
						{ label: 'Sharing a Review', slug: 'docs/guides/sharing' },
						{ label: 'Agent Collaboration', slug: 'docs/guides/agents' },
						{ label: 'Terminal Setup', slug: 'docs/guides/terminals' },
					],
				},
				{
					label: 'Reference',
					items: [
						{ label: 'Keybindings', slug: 'docs/reference/keybindings' },
						{ label: 'Configuration', slug: 'docs/reference/configuration' },
						{ label: 'CLI Reference', slug: 'docs/reference/cli' },
						{ label: 'Forge Capabilities', slug: 'docs/reference/forge-capabilities' },
						{ label: 'Themes', slug: 'docs/reference/themes' },
						{ label: 'Templates', slug: 'docs/reference/templates' },
					],
				},
				{
					label: 'Project',
					collapsed: true,
					items: [
						{ label: 'Comparisons', slug: 'docs/project/comparison' },
						{ label: 'Troubleshooting', slug: 'docs/project/troubleshooting' },
						{ label: 'Contributing', slug: 'docs/project/contributing' },
						{ label: 'Testing Against a Real Forge', slug: 'docs/contributing/live-testing' },
						{ label: 'Acknowledgements', slug: 'docs/project/acknowledgements' },
					],
				},
			],
		}),
	],
});
