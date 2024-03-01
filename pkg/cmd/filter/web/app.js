window.app = function () {
    return {
        asset: 'songs',
        query: '',
        size: 100,
        error: '',
        page: 1,
        loading: false,
        images: [],
        nav: 'home',
        nav_home: function () {
            this.nav = 'home';
            this.clear();
        },
        clear: function () {
            this.error = '';
            this.loading = false;
        },
        deleteImage: function (index) {
            id = this.images[index].id;

            console.log("deleting " + id);
            this.error = '';

            let apiURL = "/api/" + this.asset + "/" + id;
            fetch(apiURL, {
                method: 'DELETE',
                headers: {
                    'Content-Type': 'application/json'
                }
            })
                .then(response => {
                    if (response.ok) {
                        return;
                    } else {
                        throw new Error(response.statusText);
                    }
                })
                .then(data => {
                    // Remove the image from the array
                    this.images.splice(index, 1);
                })
                .catch(error => {
                    this.error = error.message;
                });
        },
        search: function (page) {
            this.page = page;
            console.log("searching");
            this.error = '';
            this.loading = false;
            this.images = [];

            // URL encode the query string
            q = encodeURIComponent(this.query);

            apiURL = "/api/"+ this.asset +"?query=" + q + "&size=" + this.size + "&page=" + this.page;

            this.loading = true;
            // Use fetch API to make a POST request to the API URL
            fetch(apiURL)
                .then(response => {
                    // Check if the response is ok (status code between 200 and 299)
                    if (response.ok) {
                        // Return the response as JSON
                        return response.json();
                    } else {
                        // Throw an error with the status text
                        throw new Error(response.statusText);
                    }
                })
                .then(data => {
                    console.log(data);
                    this.images = data;
                })
                .catch(error => {
                    // Update the component's data properties with received error and empty summary
                    this.error = error.message;
                    this.images = [];
                })
                .finally(() => {
                    this.loading = false;
                });
        },
    }
}
