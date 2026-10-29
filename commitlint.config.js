// コミットメッセージの規約。詳しくは CONTRIBUTING.md の "コミットメッセージ" を参照
// 固有名詞やコードの名前で英語が混ざるのはかまわないので、日本語が 1 文字でもあれば通す
const japanesePattern = /[\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Han}]/u;

export default {
	extends: ['@commitlint/config-conventional'],
	// 依存の更新の PR は Bot が英語で作るので、日本語の決まりを当てはめない
	ignores: [(message) => /^(chore|build|ci)\(deps\): bump /.test(message)],
	plugins: [
		{
			rules: {
				'subject-japanese': ({ subject }) => [
					!subject || japanesePattern.test(subject),
					'説明は日本語で書いてください',
				],
			},
		},
	],
	rules: {
		// 説明は日本語で書くので、大文字小文字の規則は使わない
		'subject-case': [0],
		'header-max-length': [2, 'always', 100],
		'body-max-line-length': [0],
		'footer-max-line-length': [0],
		// 説明の末尾に句点を付けない
		'subject-full-stop': [2, 'never', '。'],
		'subject-japanese': [2, 'always'],
		'scope-enum': [
			1,
			'always',
			[
				'core',
				'cli',
				'gui',
				'catalog',
				'store',
				'snapshot',
				'lifecycle',
				'windows',
				'macos',
				'linux',
				'installer',
				'release',
				'deps',
				'ci',
				'repo',
				'docs',
			],
		],
	},
};
