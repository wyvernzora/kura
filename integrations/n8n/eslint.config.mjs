import n8nCommunityNodes from '@n8n/eslint-plugin-community-nodes';
import tsParser from '@typescript-eslint/parser';

const n8nRecommended = n8nCommunityNodes.configs.recommended;

export default [
	{ ignores: ['dist/**'] },
	{
		...n8nRecommended,
		files: ['src/**/*.ts'],
		languageOptions: { parser: tsParser },
	},
	{
		...n8nRecommended,
		files: ['package.json'],
		languageOptions: {
			parser: tsParser,
			parserOptions: { extraFileExtensions: ['.json'] },
		},
	},
];
