const path = require('path');

// React, Redux and react-redux are provided by the Mattermost webapp as
// globals; bundling our own copies would break hooks and the shared store.
module.exports = {
    entry: './src/index.tsx',
    resolve: {
        extensions: ['.ts', '.tsx', '.js'],
    },
    module: {
        rules: [
            {
                test: /\.tsx?$/,
                use: 'ts-loader',
                exclude: /node_modules/,
            },
        ],
    },
    externals: {
        react: 'React',
        'react-dom': 'ReactDOM',
        redux: 'Redux',
        'react-redux': 'ReactRedux',
    },
    output: {
        path: path.join(__dirname, 'dist'),
        filename: 'main.js',
    },
    devtool: 'source-map',
};
